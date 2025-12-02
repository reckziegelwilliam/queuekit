package queue

import "testing"

func TestQueueTotalJobs(t *testing.T) {
	q := Queue{
		Size:            10,
		ProcessingCount: 5,
		CompletedCount:  100,
		FailedCount:     3,
		DeadCount:       2,
	}

	expected := int64(120)
	if total := q.TotalJobs(); total != expected {
		t.Errorf("expected TotalJobs() = %d, got %d", expected, total)
	}
}

func TestQueueHealthScore(t *testing.T) {
	tests := []struct {
		name      string
		queue     Queue
		wantScore float64
	}{
		{
			name: "perfect health",
			queue: Queue{
				CompletedCount: 100,
				FailedCount:    0,
			},
			wantScore: 100.0,
		},
		{
			name: "50% health",
			queue: Queue{
				CompletedCount: 50,
				FailedCount:    50,
			},
			wantScore: 50.0,
		},
		{
			name: "no jobs processed",
			queue: Queue{
				CompletedCount: 0,
				FailedCount:    0,
			},
			wantScore: 100.0,
		},
		{
			name: "80% health",
			queue: Queue{
				CompletedCount: 80,
				FailedCount:    20,
			},
			wantScore: 80.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := tt.queue.HealthScore()
			if score != tt.wantScore {
				t.Errorf("HealthScore() = %v, want %v", score, tt.wantScore)
			}
		})
	}
}
