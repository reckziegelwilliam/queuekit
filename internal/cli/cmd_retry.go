package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "retry <job-id>",
		Short: "Retry a failed or dead job",
		Args:  cobra.ExactArgs(1),
		RunE:  runRetry,
	})
}

func runRetry(cmd *cobra.Command, args []string) error {
	resp, err := client.do(http.MethodPost, "/v1/jobs/"+args[0]+"/retry", nil)
	if err != nil {
		return err
	}

	var job struct {
		ID string `json:"id"`
	}
	if err := client.decodeOrError(resp, &job); err != nil {
		return err
	}

	fmt.Printf("Job retried. New job ID: %s\n", job.ID)
	return nil
}
