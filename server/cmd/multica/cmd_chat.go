package main

import (
	"context"
	"github.com/multica-ai/multica/server/internal/cli"
	"github.com/spf13/cobra"
	"net/url"
	"os"
)

// These read-only commands use the server's session ownership/private-agent gates.
func init() {
	chat := &cobra.Command{Use: "chat", Short: "Inspect your chat sessions"}
	list := &cobra.Command{Use: "list", Short: "List your chat sessions", Args: exactArgs(0), RunE: runChatRead}
	get := &cobra.Command{Use: "get <id>", Short: "Get your chat session", Args: exactArgs(1), RunE: runChatRead}
	messages := &cobra.Command{Use: "messages <id>", Short: "Read messages in your chat session", Args: exactArgs(1), RunE: runChatRead}
	for _, c := range []*cobra.Command{list, get, messages} {
		c.Flags().String("output", "json", "Output format: json")
		chat.AddCommand(c)
	}
	rootCmd.AddCommand(chat)
}
func runChatRead(cmd *cobra.Command, args []string) error {
	client, err := newAPIClient(cmd)
	if err != nil {
		return err
	}
	if _, err := requireWorkspaceID(cmd); err != nil {
		return err
	}
	ctx, cancel := cli.APIContext(context.Background())
	defer cancel()
	path := "/api/chat-sessions"
	if len(args) > 0 {
		path += "/" + url.PathEscape(args[0])
		if cmd.Name() == "messages" {
			path += "/messages"
		}
	}
	var result any
	if err := client.GetJSON(ctx, path, &result); err != nil {
		return err
	}
	return cli.PrintJSON(os.Stdout, result)
}
