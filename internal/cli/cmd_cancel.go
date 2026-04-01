package cli

import (
	"fmt"
	"net/http"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{
		Use:   "cancel <job-id>",
		Short: "Cancel a pending job",
		Args:  cobra.ExactArgs(1),
		RunE:  runCancel,
	})
}

func runCancel(cmd *cobra.Command, args []string) error {
	resp, err := client.do(http.MethodPost, "/v1/jobs/"+args[0]+"/cancel", nil)
	if err != nil {
		return err
	}

	if err := client.decodeOrError(resp, nil); err != nil {
		return err
	}

	fmt.Println("Job cancelled.")
	return nil
}
