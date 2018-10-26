package flink

import (
	"context"

	"errors"
	"fmt"

	"github.com/lyft/flinkk8soperator/pkg/apis/app/v1alpha1"
	"github.com/lyft/flinkk8soperator/pkg/controller/flink/client"
	"github.com/lyft/flinkk8soperator/pkg/controller/k8"
	"k8s.io/api/apps/v1"
)

// Interface to manage Flink Application in Kubernetes
type FlinkInterface interface {
	// Creates a Flink cluster with necessary Job Manager, Task Managers and services for UI
	CreateCluster(ctx context.Context, application *v1alpha1.FlinkApplication) error

	// Deletes a Flink cluster that does not match the spec of the Application if there is one
	DeleteOldCluster(ctx context.Context, application *v1alpha1.FlinkApplication, deleteFrontEnd bool) error

	// Cancels the running/active jobs in the Cluster for the Application after savepoint is created
	CancelWithSavepoint(ctx context.Context, application *v1alpha1.FlinkApplication) (string, error)

	// Starts the Job in the Flink Cluster based on values in the Application
	StartFlinkJob(ctx context.Context, application *v1alpha1.FlinkApplication) (string, error)

	// Savepoint creation is asynchronous.
	// Polls the status of the Savepoint, using the triggerId
	GetSavepointStatus(ctx context.Context, application *v1alpha1.FlinkApplication) (*client.SavepointResponse, error)

	// Check if the Flink Kubernetes Cluster is Ready.
	// Checks if all the pods of task and job managers are ready.
	IsClusterReady(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error)

	// Checks to see if the Flink Cluster is ready to handle API requests
	IsServiceReady(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error)

	// Compares the Application Spec with the underlying Flink Cluster.
	// Return true if the spec does not match the underlying Flink cluster.
	HasApplicationChanged(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error)

	// Indicates if a new Flink cluster needs to spinned up for the Application
	IsClusterChangeNeeded(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error)

	// Ensure that correct number of Task managers are running for the Application.
	CheckAndUpdateTaskManager(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error)

	// Check if the Parallelism of the Application matches the parallelism of the Job running in the cluster
	IsApplicationParallelismDifferent(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error)

	// Check if multiple Flink clusters are running for the Application
	IsMultipleClusterPresent(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error)

	// Returns the list of Jobs running on the Flink Cluster for the Application
	GetJobsForApplication(ctx context.Context, application *v1alpha1.FlinkApplication) ([]client.FlinkJob, error)
}

func NewFlinkController() FlinkInterface {
	return &FlinkController{
		k8Cluster:        k8.NewK8Cluster(),
		flinkJobManager:  NewFlinkJobManagerController(),
		FlinkTaskManager: NewFlinkTaskManagerController(),
		flinkClient:      client.NewFlinkJobManagerClient(),
	}
}

type FlinkController struct {
	k8Cluster        k8.K8ClusterInterface
	flinkJobManager  FlinkJobManagerControllerInterface
	FlinkTaskManager FlinkTaskManagerControllerInterface
	flinkClient      client.FlinkAPIInterface
}

func GetActiveFlinkJob(jobs []client.FlinkJob) *client.FlinkJob {
	if len(jobs) == 0 {
		return nil
	}
	for _, job := range jobs {
		if job.Status == client.FlinkJobRunning ||
			job.Status == client.FlinkJobCreated {
			return &job
		}
	}
	return nil
}

func (f *FlinkController) IsApplicationParallelismDifferent(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error) {
	serviceName := getJobManagerServiceName(*application)
	jobId, err := f.getJobIdForApplication(ctx, application)
	if err != nil {
		return false, err
	}
	jobConfig, err := f.flinkClient.GetJobConfig(ctx, serviceName, jobId)
	if application.Spec.Parallelism != jobConfig.ExecutionConfig.Parallelism {
		return true, nil
	}
	return false, nil
}

func (f *FlinkController) CheckAndUpdateTaskManager(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error) {
	currentAppDeployments, err := f.getDeploymentsForApp(ctx, application)
	if err != nil {
		return false, err
	}
	taskManagerDeployment := getTaskManagerDeployment(currentAppDeployments.Items, application)
	if *taskManagerDeployment.Spec.Replicas != application.Spec.NumberTaskManagers {
		taskManagerDeployment.Spec.Replicas = &application.Spec.NumberTaskManagers
		err := f.k8Cluster.UpdateK8Object(ctx, taskManagerDeployment)
		if err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (f *FlinkController) IsMultipleClusterPresent(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error) {
	currentDeployments, oldDeployments, err := f.getCurrentAndOldDeploymentsForApp(ctx, application)
	if err != nil {
		return false, err
	}
	if len(currentDeployments) > 0 && len(oldDeployments) > 0 {
		return true, nil
	}
	return false, nil
}

func (f *FlinkController) getDeploymentsForApp(ctx context.Context, application *v1alpha1.FlinkApplication) (*v1.DeploymentList, error) {
	appLabels := k8.GetAppLabel(application.Name)
	return f.k8Cluster.GetDeploymentsWithLabel(ctx, application.Namespace, appLabels)
}

func (f *FlinkController) getDeploymentsForImage(ctx context.Context, application *v1alpha1.FlinkApplication) (*v1.DeploymentList, error) {
	imageLabels := k8.GetImageLabel(k8.GetImageKey(application.Spec.Image))
	return f.k8Cluster.GetDeploymentsWithLabel(ctx, application.Namespace, imageLabels)
}

func (f *FlinkController) GetJobsForApplication(ctx context.Context, application *v1alpha1.FlinkApplication) ([]client.FlinkJob, error) {
	serviceName := getJobManagerServiceName(*application)
	jobResponse, err := f.flinkClient.GetJobs(ctx, serviceName)
	if err != nil {
		return nil, err
	}
	return jobResponse.Jobs, nil
}

// The operator for now assumes and is intended to run single application per Flink Cluster.
// Once we move to run multiple applications, this has to be removed/updated
func (f *FlinkController) getJobIdForApplication(ctx context.Context, application *v1alpha1.FlinkApplication) (string, error) {
	if application.Status.ActiveJobId != "" {
		return application.Status.ActiveJobId, nil
	}
	// The logic below is not needed but just a safety net if job id is not persisted in the CRD
	jobs, err := f.GetJobsForApplication(ctx, application)
	if err != nil {
		return "", err
	}
	activeJob := GetActiveFlinkJob(jobs)
	if activeJob == nil {
		return "", errors.New(fmt.Sprintf("invalid jobs %v", jobs))
	}
	return activeJob.JobId, nil
}

func (f *FlinkController) CancelWithSavepoint(ctx context.Context, application *v1alpha1.FlinkApplication) (string, error) {
	serviceName := getJobManagerServiceName(*application)
	jobId, err := f.getJobIdForApplication(ctx, application)
	if err != nil {
		return "", err
	}
	return f.flinkClient.CancelJobWithSavepoint(ctx, serviceName, jobId)
}

func (f *FlinkController) CreateCluster(ctx context.Context, application *v1alpha1.FlinkApplication) error {
	err := f.flinkJobManager.CreateIfNotExist(ctx, application)
	if err != nil {
		return err
	}
	err = f.FlinkTaskManager.CreateIfNotExist(ctx, application)
	if err != nil {
		return err
	}
	return nil
}

func (f *FlinkController) StartFlinkJob(ctx context.Context, application *v1alpha1.FlinkApplication) (string, error) {
	serviceName := getJobManagerServiceName(*application)
	response, err := f.flinkClient.SubmitJob(
		ctx,
		serviceName,
		application.JobJarName,
		application.SavepointInfo.SavepointLocation,
		application.Spec.Parallelism)
	if err != nil {
		return "", err
	}
	if response.JobId == "" {
		return "", errors.New("unable to submit job: invalid job id")
	}
	return response.JobId, nil
}

func (f *FlinkController) GetSavepointStatus(ctx context.Context, application *v1alpha1.FlinkApplication) (*client.SavepointResponse, error) {
	serviceName := getJobManagerServiceName(*application)
	jobId, err := f.getJobIdForApplication(ctx, application)
	if err != nil {
		return nil, err
	}
	return f.flinkClient.CheckSavepointStatus(ctx, serviceName, jobId, application.SavepointInfo.TriggerId)
}

func (f *FlinkController) DeleteOldCluster(ctx context.Context, application *v1alpha1.FlinkApplication, deleteFrontEnd bool) error {
	_, oldDeployments, err := f.getCurrentAndOldDeploymentsForApp(ctx, application)
	if err != nil {
		return err
	}
	if len(oldDeployments) == 0 {
		return nil
	}
	err = f.k8Cluster.DeleteDeployments(ctx, v1.DeploymentList{
		Items: oldDeployments,
	})
	if err != nil {
		return err
	}
	return nil
}

func (f *FlinkController) IsClusterReady(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error) {
	appImageLabel := k8.GetImageLabel(k8.GetImageKey(application.Spec.Image))
	return f.k8Cluster.IsAllPodsRunning(ctx, application.Namespace, appImageLabel)
}

func (f *FlinkController) IsServiceReady(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error) {
	serviceName := getJobManagerServiceName(*application)
	_, err := f.flinkClient.GetClusterOverview(ctx, serviceName)
	if err != nil {
		return false, err
	}
	return true, nil
}

func (f *FlinkController) getCurrentAndOldDeploymentsForApp(ctx context.Context, application *v1alpha1.FlinkApplication) ([]v1.Deployment, []v1.Deployment, error) {
	currentAppDeployments, err := f.getDeploymentsForApp(ctx, application)
	if err != nil {
		return nil, nil, err
	}
	appImageLabel := k8.GetImageLabel(k8.GetImageKey(application.Spec.Image))
	currentDeployments, oldDeployments := k8.MatchDeploymentsByLabel(*currentAppDeployments, appImageLabel)
	return currentDeployments, oldDeployments, nil
}

func (f *FlinkController) HasApplicationChanged(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error) {
	clusterChangeNeeded, err := f.IsClusterChangeNeeded(ctx, application)
	if err != nil {
		return false, err
	}
	if clusterChangeNeeded {
		return true, nil
	}

	clusterUpdateNeeded, err := f.isClusterUpdateNeeded(ctx, application)
	if err != nil {
		return false, err
	}

	if clusterUpdateNeeded {
		return true, nil
	}
	return false, nil
}

func (f *FlinkController) IsClusterChangeNeeded(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error) {
	currentDeployments, err := f.getDeploymentsForImage(ctx, application)
	if err != nil {
		return false, err
	}
	if len(currentDeployments.Items) == 0 {
		return true, nil
	}
	return false, nil
}

func (f *FlinkController) isClusterUpdateNeeded(ctx context.Context, application *v1alpha1.FlinkApplication) (bool, error) {
	currentAppDeployments, err := f.getDeploymentsForApp(ctx, application)
	if err != nil {
		return false, err
	}
	taskManagerReplicaCount := getTaskManagerReplicaCount(currentAppDeployments.Items, application)
	if taskManagerReplicaCount != application.Spec.NumberTaskManagers {
		return true, nil
	}
	return f.IsApplicationParallelismDifferent(ctx, application)
}
