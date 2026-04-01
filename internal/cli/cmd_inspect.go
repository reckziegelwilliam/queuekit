package cli

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func init() {
	inspectCmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect queues and jobs",
	}

	queuesCmd := &cobra.Command{
		Use:   "queues",
		Short: "List all queues with statistics",
		RunE:  runInspectQueues,
	}

	jobsCmd := &cobra.Command{
		Use:   "jobs",
		Short: "List jobs in a queue",
		RunE:  runInspectJobs,
	}
	jobsCmd.Flags().String("queue", "", "queue name (required)")
	jobsCmd.Flags().String("status", "", "filter by status")
	jobsCmd.Flags().Int("limit", 20, "number of jobs to show")
	_ = jobsCmd.MarkFlagRequired("queue")

	inspectCmd.AddCommand(queuesCmd, jobsCmd)
	rootCmd.AddCommand(inspectCmd)
}

func runInspectQueues(cmd *cobra.Command, args []string) error {
	resp, err := client.do(http.MethodGet, "/v1/queues", nil)
	if err != nil {
		return err
	}

	var queues []struct {
		Name            string `json:"name"`
		Size            int64  `json:"size"`
		ProcessingCount int64  `json:"processing_count"`
		CompletedCount  int64  `json:"completed_count"`
		FailedCount     int64  `json:"failed_count"`
		DeadCount       int64  `json:"dead_count"`
	}
	if err := client.decodeOrError(resp, &queues); err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tPENDING\tRUNNING\tCOMPLETED\tFAILED\tDEAD")
	for _, q := range queues {
		_, _ = fmt.Fprintf(w, "%s\t%d\t%d\t%d\t%d\t%d\n",
			q.Name, q.Size, q.ProcessingCount, q.CompletedCount, q.FailedCount, q.DeadCount)
	}
	_ = w.Flush()
	return nil
}

func runInspectJobs(cmd *cobra.Command, args []string) error {
	queueName, _ := cmd.Flags().GetString("queue")
	status, _ := cmd.Flags().GetString("status")
	limit, _ := cmd.Flags().GetInt("limit")

	params := url.Values{}
	if status != "" {
		params.Set("status", status)
	}
	params.Set("limit", strconv.Itoa(limit))

	path := "/v1/queues/" + url.PathEscape(queueName) + "/jobs"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	resp, err := client.do(http.MethodGet, path, nil)
	if err != nil {
		return err
	}

	var jobs []struct {
		ID        string `json:"id"`
		Type      string `json:"type"`
		Status    string `json:"status"`
		Attempts  int    `json:"attempts"`
		CreatedAt string `json:"created_at"`
		LastError string `json:"last_error"`
	}
	if err := client.decodeOrError(resp, &jobs); err != nil {
		return err
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tTYPE\tSTATUS\tATTEMPTS\tCREATED\tERROR")
	for _, j := range jobs {
		id := j.ID
		if len(id) > 8 {
			id = id[:8]
		}
		errMsg := j.LastError
		if len(errMsg) > 40 {
			errMsg = errMsg[:40] + "..."
		}
		if errMsg == "" {
			errMsg = "-"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
			id, j.Type, j.Status, j.Attempts, j.CreatedAt, errMsg)
	}
	_ = w.Flush()
	return nil
}
