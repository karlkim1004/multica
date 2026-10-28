package main

import (
	"context"
	"fmt"
	"net/url"
	"os"

	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
)

var taskCmd = &cobra.Command{Use: "task", Short: "Inspect tasks"}
var taskUsageCmd = &cobra.Command{
	Use: "usage <task-id>", Short: "Show reported token usage for a task", Args: exactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := newAPIClient(cmd)
		if err != nil {
			return err
		}
		ctx, cancel := cli.APIContext(context.Background())
		defer cancel()
		var usage []map[string]any
		if err := client.GetJSON(ctx, "/api/tasks/"+url.PathEscape(args[0])+"/usage", &usage); err != nil {
			return fmt.Errorf("get task usage: %w", err)
		}
		output, _ := cmd.Flags().GetString("output")
		if output == "json" {
			return cli.PrintJSON(os.Stdout, usage)
		}
		rows := make([][]string, 0, len(usage))
		for _, u := range usage {
			rows = append(rows, []string{strVal(u, "provider"), strVal(u, "model"), formatMetadataValue(u["input_tokens"]), formatMetadataValue(u["output_tokens"]), formatMetadataValue(u["cache_read_tokens"]), formatMetadataValue(u["cache_write_tokens"])})
		}
		cli.PrintTable(os.Stdout, []string{"PROVIDER", "MODEL", "INPUT_TOKENS", "OUTPUT_TOKENS", "CACHE_READ", "CACHE_WRITE"}, rows)
		return nil
	},
}

func init() {
	taskUsageCmd.Flags().String("output", "table", "Output format: table or json")
	taskCmd.AddCommand(taskUsageCmd)
	rootCmd.AddCommand(taskCmd)
}
