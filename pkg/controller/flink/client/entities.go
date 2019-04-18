package client

type SavepointStatus string

const (
	SavePointInvalid    SavepointStatus = ""
	SavePointInProgress SavepointStatus = "IN_PROGRESS"
	SavePointCompleted  SavepointStatus = "COMPLETED"
)

type CheckpointStatus string

const (
	CheckpointInProgress CheckpointStatus = "IN_PROGRESS"
	CheckpointFailed     CheckpointStatus = "FAILED"
	CheckpointCompleted  CheckpointStatus = "COMPLETED"
)

type FlinkJobStatus string

const (
	FlinkJobCreated    FlinkJobStatus = "CREATED"
	FlinkJobRunning    FlinkJobStatus = "RUNNING"
	FlinkJobFailing    FlinkJobStatus = "FAILING"
	FlinkJobFailed     FlinkJobStatus = "FAILED"
	FlinkJobCancelling FlinkJobStatus = "CANCELLING"
	FlinkJobCanceled   FlinkJobStatus = "CANCELED"
	FlinkJobFinished   FlinkJobStatus = "FINISHED"
)

type CancelJobRequest struct {
	CancelJob       bool   `json:"cancel-job"`
	TargetDirectory string `json:"target-directory,omitempty"`
}

type SubmitJobRequest struct {
	SavepointPath string `json:"savepointPath"`
	Parallelism   int32  `json:"parallelism"`
	ProgramArgs   string `json:"programArgs"`
	EntryClass    string `json:"entryClass"`
}

type SavepointResponse struct {
	SavepointStatus SavepointStatusResponse    `json:"status"`
	Operation       SavepointOperationResponse `json:"operation"`
}

type SavepointStatusResponse struct {
	Status SavepointStatus `json:"id"`
}

type SavepointOperationResponse struct {
	Location     string       `json:"location"`
	FailureCause FailureCause `json"failure-cause"`
}

type FailureCause struct {
	Class      string `json:"class"`
	StackTrace string `json:"stack-trace"`
}

type CancelJobResponse struct {
	TriggerId string `json:"request-id"`
}

type SubmitJobResponse struct {
	JobId string `json:"jobid"`
}

type GetJobsResponse struct {
	Jobs []FlinkJob `json:"jobs"`
}

type JobConfigResponse struct {
	JobId           string             `json:"jid"`
	ExecutionConfig JobExecutionConfig `json:"execution-config"`
}

type JobExecutionConfig struct {
	Parallelism int32 `json:"job-parallelism"`
}

type FlinkJob struct {
	JobId  string         `json:"id"`
	Status FlinkJobStatus `json:"status"`
}

type ClusterOverviewResponse struct {
	TaskManagerCount uint `json:"taskmanagers"`
	SlotsAvailable   uint `json:"slots-available"`
}

type CheckpointStatistics struct {
	Id                 uint             `json:"id"`
	Status             CheckpointStatus `json:"status"`
	IsSavepoint        bool             `json:"is_savepoint"`
	TriggerTimestamp   int64            `json:"trigger_timestamp"`
	LatestAckTimestamp int64            `json:"latest_ack_timestamp"`
	StateSize          int64            `json:"state_size"`
	EndToEndDuration   int64            `json:"end_to_end_duration"`
	AlignmentBuffered  int64            `json:"alignment_buffered"`
	NumSubtasks        int64            `json:"num_subtasks"`
	FailureTimestamp   int64            `json:"failure_timestamp"`
	FailureMessage     string           `json:"failure_message"`
	ExternalPath       string           `json:"external_path"`
	Discarded          bool             `json:"discarded"`
}

type LatestCheckpoints struct {
	Completed *CheckpointStatistics `json:"completed,omitempty"`
	Savepoint *CheckpointStatistics `json:"savepoint,omitempty"`
	Failed    *CheckpointStatistics `json:"failed,omitempty"`
	Restored  *CheckpointStatistics `json:"restored,omitempty"`
}

type CheckpointResponse struct {
	Counts  map[string]int32       `json:"counts"`
	Latest  LatestCheckpoints      `json:"latest"`
	History []CheckpointStatistics `json:"history"`
}
