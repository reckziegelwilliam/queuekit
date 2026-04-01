package cli

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func init() {
	cmd := &cobra.Command{
		Use:   "enqueue <type>",
		Short: "Enqueue a new job",
		Args:  cobra.ExactArgs(1),
		RunE:  runEnqueue,
	}

	cmd.Flags().String("queue", "default", "target queue name")
	cmd.Flags().String("payload", "{}", "JSON payload")
	cmd.Flags().Int("priority", 10, "job priority (0=low, 10=normal, 20=high, 30=critical)")
	cmd.Flags().Int("max-attempts", 3, "maximum retry attempts")

	rootCmd.AddCommand(cmd)
}

func runEnqueue(cmd *cobra.Command, args []string) error {
	queueName, _ := cmd.Flags().GetString("queue")
	payloadStr, _ := cmd.Flags().GetString("payload")
	priority, _ := cmd.Flags().GetInt("priority")
	maxAttempts, _ := cmd.Flags().GetInt("max-attempts")

	if !json.Valid([]byte(payloadStr)) {
		return fmt.Errorf("invalid JSON payload: %s", payloadStr)
	}

	body := map[string]any{
		"type":         args[0],
		"queue":        queueName,
		"payload":      json.RawMessage(payloadStr),
		"priority":     priority,
		"max_attempts": maxAttempts,
	}

	resp, err := client.do(http.MethodPost, "/v1/jobs", body)
	if err != nil {
		return err
	}

	var job struct {
		ID string `json:"id"`
	}
	if err := client.decodeOrError(resp, &job); err != nil {
		return err
	}

	fmt.Printf("Job enqueued: %s\n", job.ID)
	return nil
}
