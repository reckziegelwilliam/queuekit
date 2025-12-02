package queue

// Queue represents a job queue with statistics
type Queue struct {
	Name            string `json:"name"`
	Size            int64  `json:"size"`             // Total pending jobs
	ProcessingCount int64  `json:"processing_count"` // Currently running jobs
	CompletedCount  int64  `json:"completed_count"`  // Total completed jobs
	FailedCount     int64  `json:"failed_count"`     // Total failed jobs
	DeadCount       int64  `json:"dead_count"`       // Jobs in dead-letter queue
}

// TotalJobs returns the total number of jobs across all statuses
func (q *Queue) TotalJobs() int64 {
	return q.Size + q.ProcessingCount + q.CompletedCount + q.FailedCount + q.DeadCount
}

// HealthScore returns a simple health score (0-100) based on success rate
func (q *Queue) HealthScore() float64 {
	total := q.CompletedCount + q.FailedCount
	if total == 0 {
		return 100.0
	}
	return (float64(q.CompletedCount) / float64(total)) * 100.0
}
