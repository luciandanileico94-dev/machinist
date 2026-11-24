package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

func newFactoryCommand() *cobra.Command {
	root := &cobra.Command{Use: "factory", Hidden: true}
	upgrade := &cobra.Command{
		Use:  "upgrade",
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errors.New("своя фабрика — upgrade выключен")
		},
	}
	upgrade.Flags().Bool("force", false, "legacy flag")
	root.AddCommand(upgrade)
	return root
}
