package main

import "github.com/spf13/cobra"

// inject feeds a synthetic event into the engine — for tests, scripts,
// and CI systems that want to emit events without a watcher.
func injectCmd() *cobra.Command {
	var evType, source string
	var payload map[string]string
	cmd := &cobra.Command{
		Use:   "inject",
		Short: "Inject an event (testing / external producers)",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := dial()
			if err != nil {
				return err
			}
			var res map[string]any
			if err := c.Call("event.inject", map[string]any{
				"type": evType, "source": source, "payload": payload,
			}, &res); err != nil {
				return err
			}
			return printJSON(res)
		},
	}
	cmd.Flags().StringVar(&evType, "type", "", "event type (required)")
	cmd.Flags().StringVar(&source, "source", "", "event source")
	cmd.Flags().StringToStringVar(&payload, "payload", nil, "payload k=v pairs")
	cmd.MarkFlagRequired("type")
	return cmd
}
