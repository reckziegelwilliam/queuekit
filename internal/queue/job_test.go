package queue

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNewJob(t *testing.T) {
	payload := json.RawMessage(`{"email": "test@example.com"}`)
	job := NewJob("email.send", "emails", payload)

	if job.ID == "" {
		t.Error("expected job ID to be generated")
	}
	if job.Type != "email.send" {
		t.Errorf("expected type 'email.send', got '%s'", job.Type)
	}
	if job.Queue != "emails" {
		t.Errorf("expected queue 'emails', got '%s'", job.Queue)
	}
	if job.Status != StatusPending {
		t.Errorf("expected status 'pending', got '%s'", job.Status)
	}
	if job.Priority != PriorityNormal {
		t.Errorf("expected priority %d, got %d", PriorityNormal, job.Priority)
	}
	if job.Attempts != 0 {
		t.Errorf("expected attempts 0, got %d", job.Attempts)
	}
	if job.MaxAttempts != 3 {
		t.Errorf("expected max_attempts 3, got %d", job.MaxAttempts)
	}
}

func TestJobValidate(t *testing.T) {
	tests := []struct {
		name    string
		job     *Job
		wantErr bool
	}{
		{
			name: "valid job",
			job: &Job{
				Type:        "test.job",
				Queue:       "default",
				Payload:     json.RawMessage(`{}`),
				MaxAttempts: 3,
			},
			wantErr: false,
		},
		{
			name: "missing type",
			job: &Job{
				Queue:       "default",
				Payload:     json.RawMessage(`{}`),
				MaxAttempts: 3,
			},
			wantErr: true,
		},
		{
			name: "missing queue",
			job: &Job{
				Type:        "test.job",
				Payload:     json.RawMessage(`{}`),
				MaxAttempts: 3,
			},
			wantErr: true,
		},
		{
			name: "missing payload",
			job: &Job{
				Type:        "test.job",
				Queue:       "default",
				MaxAttempts: 3,
			},
			wantErr: true,
		},
		{
			name: "invalid max_attempts",
			job: &Job{
				Type:        "test.job",
				Queue:       "default",
				Payload:     json.RawMessage(`{}`),
				MaxAttempts: 0,
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.job.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestJobIsRetryable(t *testing.T) {
	tests := []struct {
		name        string
		attempts    int
		maxAttempts int
		want        bool
	}{
		{"no attempts yet", 0, 3, true},
		{"one attempt", 1, 3, true},
		{"at max attempts", 3, 3, false},
		{"exceeded max attempts", 4, 3, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job := &Job{
				Attempts:    tt.attempts,
				MaxAttempts: tt.maxAttempts,
			}
			if got := job.IsRetryable(); got != tt.want {
				t.Errorf("IsRetryable() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestJobMarkRunning(t *testing.T) {
	job := &Job{Status: StatusPending}
	before := time.Now().UTC()
	job.MarkRunning()

	if job.Status != StatusRunning {
		t.Errorf("expected status 'running', got '%s'", job.Status)
	}
	if job.UpdatedAt.Before(before) {
		t.Error("expected UpdatedAt to be updated")
	}
}

func TestJobMarkCompleted(t *testing.T) {
	job := &Job{Status: StatusRunning}
	before := time.Now().UTC()
	job.MarkCompleted()

	if job.Status != StatusCompleted {
		t.Errorf("expected status 'completed', got '%s'", job.Status)
	}
	if job.CompletedAt == nil {
		t.Error("expected CompletedAt to be set")
	}
	if job.CompletedAt.Before(before) {
		t.Error("expected CompletedAt to be recent")
	}
	if job.UpdatedAt.Before(before) {
		t.Error("expected UpdatedAt to be updated")
	}
}

func TestJobMarkFailed(t *testing.T) {
	job := &Job{
		Status:   StatusRunning,
		Attempts: 0,
	}
	before := time.Now().UTC()
	testErr := &testError{msg: "test error"}
	job.MarkFailed(testErr)

	if job.Status != StatusFailed {
		t.Errorf("expected status 'failed', got '%s'", job.Status)
	}
	if job.Attempts != 1 {
		t.Errorf("expected attempts 1, got %d", job.Attempts)
	}
	if job.LastError != "test error" {
		t.Errorf("expected LastError 'test error', got '%s'", job.LastError)
	}
	if job.FailedAt == nil {
		t.Error("expected FailedAt to be set")
	}
	if job.FailedAt.Before(before) {
		t.Error("expected FailedAt to be recent")
	}
	if job.UpdatedAt.Before(before) {
		t.Error("expected UpdatedAt to be updated")
	}
}

func TestJobMarkDead(t *testing.T) {
	job := &Job{Status: StatusFailed}
	before := time.Now().UTC()
	job.MarkDead()

	if job.Status != StatusDead {
		t.Errorf("expected status 'dead', got '%s'", job.Status)
	}
	if job.UpdatedAt.Before(before) {
		t.Error("expected UpdatedAt to be updated")
	}
}

func TestJobSerialization(t *testing.T) {
	original := NewJob("test.job", "default", json.RawMessage(`{"key":"value"}`))
	original.Priority = PriorityHigh

	// Serialize to JSON
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("failed to marshal job: %v", err)
	}

	// Deserialize from JSON
	var decoded Job
	err = json.Unmarshal(data, &decoded)
	if err != nil {
		t.Fatalf("failed to unmarshal job: %v", err)
	}

	// Compare key fields
	if decoded.ID != original.ID {
		t.Errorf("expected ID %s, got %s", original.ID, decoded.ID)
	}
	if decoded.Type != original.Type {
		t.Errorf("expected Type %s, got %s", original.Type, decoded.Type)
	}
	if decoded.Queue != original.Queue {
		t.Errorf("expected Queue %s, got %s", original.Queue, decoded.Queue)
	}
	if decoded.Priority != original.Priority {
		t.Errorf("expected Priority %d, got %d", original.Priority, decoded.Priority)
	}
	if string(decoded.Payload) != string(original.Payload) {
		t.Errorf("expected Payload %s, got %s", original.Payload, decoded.Payload)
	}
}

// Helper test error type
type testError struct {
	msg string
}

func (e *testError) Error() string {
	return e.msg
}

