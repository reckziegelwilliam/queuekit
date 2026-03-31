package redis

import "github.com/redis/go-redis/v9"

// Lua script to atomically reserve a job from a queue
// KEYS[1] = queue sorted set key (e.g., "queue:emails")
// KEYS[2] = job hash key prefix (e.g., "job:")
// ARGV[1] = current timestamp
// ARGV[2] = new status ("running")
// ARGV[3] = updated_at timestamp
var reserveScript = redis.NewScript(`
	-- Pop the job with the lowest score (earliest scheduled_at) that's ready
	local jobs = redis.call('ZRANGEBYSCORE', KEYS[1], '-inf', ARGV[1], 'LIMIT', 0, 1)
	if #jobs == 0 then
		return nil
	end
	
	local job_id = jobs[1]
	local job_key = KEYS[2] .. job_id
	
	-- Check if job exists and is still pending
	local status = redis.call('HGET', job_key, 'status')
	if status ~= 'pending' then
		-- Remove from queue and return nil
		redis.call('ZREM', KEYS[1], job_id)
		return nil
	end
	
	-- Update job status to running
	redis.call('HSET', job_key, 'status', ARGV[2], 'updated_at', ARGV[3])
	
	-- Remove from pending queue
	redis.call('ZREM', KEYS[1], job_id)
	
	-- Add to running set for tracking
	redis.call('SADD', 'status:' .. KEYS[1] .. ':running', job_id)
	redis.call('SREM', 'status:' .. KEYS[1] .. ':pending', job_id)
	
	-- Return all job fields
	return redis.call('HGETALL', job_key)
`)

// Lua script to atomically nack a job
// KEYS[1] = job hash key
// KEYS[2] = queue name
// ARGV[1] = current attempts (before increment)
// ARGV[2] = max_attempts
// ARGV[3] = last_error
// ARGV[4] = current timestamp (unix seconds)
// ARGV[5] = retry_at timestamp (unix seconds, now + retryDelay)
var nackScript = redis.NewScript(`
	local job_key = KEYS[1]
	local queue_name = KEYS[2]
	local attempts = tonumber(ARGV[1]) + 1
	local max_attempts = tonumber(ARGV[2])
	local last_error = ARGV[3]
	local now = ARGV[4]
	local retry_at = tonumber(ARGV[5])

	-- Get job ID
	local job_id = redis.call('HGET', job_key, 'id')
	if not job_id then
		return redis.error_reply('job not found')
	end

	-- Update attempts and error
	redis.call('HSET', job_key,
		'attempts', attempts,
		'last_error', last_error,
		'failed_at', now,
		'updated_at', now
	)

	-- Remove from running set
	redis.call('SREM', 'status:queue:' .. queue_name .. ':running', job_id)

	-- Check if exceeded max attempts
	if attempts >= max_attempts then
		-- Move to dead letter queue
		redis.call('HSET', job_key, 'status', 'dead')
		redis.call('SADD', 'status:queue:' .. queue_name .. ':dead', job_id)
		return 'dead'
	else
		-- Re-schedule as pending with backoff delay so Reserve can pick it up
		redis.call('HSET', job_key, 'status', 'pending', 'scheduled_at', retry_at)
		redis.call('ZADD', 'queue:' .. queue_name, retry_at, job_id)
		redis.call('SADD', 'status:queue:' .. queue_name .. ':pending', job_id)
		return 'pending'
	end
`)

// Lua script to get queue statistics
// KEYS[1] = queue name
var queueStatsScript = redis.NewScript(`
	local queue_name = KEYS[1]
	local stats = {}
	
	stats[1] = 'name'
	stats[2] = queue_name
	stats[3] = 'size'
	stats[4] = redis.call('ZCARD', 'queue:' .. queue_name)
	stats[5] = 'processing_count'
	stats[6] = redis.call('SCARD', 'status:queue:' .. queue_name .. ':running')
	stats[7] = 'completed_count'
	stats[8] = redis.call('SCARD', 'status:queue:' .. queue_name .. ':completed')
	stats[9] = 'failed_count'
	stats[10] = redis.call('SCARD', 'status:queue:' .. queue_name .. ':failed')
	stats[11] = 'dead_count'
	stats[12] = redis.call('SCARD', 'status:queue:' .. queue_name .. ':dead')
	
	return stats
`)
