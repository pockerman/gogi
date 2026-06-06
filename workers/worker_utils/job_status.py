from enum import StrEnum 

class  JobStatus(StrEnum):

    JobPending = "PENDING"
    JobRunning = "RUNNING"
    JobCompleted = "COMPLETED"
    JobFailed    = "FAILED"