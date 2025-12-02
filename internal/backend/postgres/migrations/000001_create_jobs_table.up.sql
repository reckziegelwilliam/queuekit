-- Create jobs table
CREATE TABLE IF NOT EXISTS jobs (
    id VARCHAR(36) PRIMARY KEY,
    type VARCHAR(255) NOT NULL,
    queue VARCHAR(255) NOT NULL,
    payload JSONB NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    priority INTEGER NOT NULL DEFAULT 10,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 3,
    scheduled_at TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL,
    completed_at TIMESTAMP WITH TIME ZONE,
    failed_at TIMESTAMP WITH TIME ZONE,
    last_error TEXT
);

-- Create indexes for efficient job retrieval
CREATE INDEX IF NOT EXISTS idx_jobs_queue_status_scheduled 
    ON jobs(queue, status, scheduled_at) 
    WHERE status IN ('pending', 'running');

CREATE INDEX IF NOT EXISTS idx_jobs_status 
    ON jobs(status);

CREATE INDEX IF NOT EXISTS idx_jobs_queue 
    ON jobs(queue);

CREATE INDEX IF NOT EXISTS idx_jobs_created_at 
    ON jobs(created_at DESC);

CREATE INDEX IF NOT EXISTS idx_jobs_type 
    ON jobs(type);

-- Add check constraints
ALTER TABLE jobs ADD CONSTRAINT chk_status 
    CHECK (status IN ('pending', 'running', 'completed', 'failed', 'dead'));

ALTER TABLE jobs ADD CONSTRAINT chk_max_attempts 
    CHECK (max_attempts >= 1);

ALTER TABLE jobs ADD CONSTRAINT chk_attempts 
    CHECK (attempts >= 0);

