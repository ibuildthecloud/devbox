// Copyright 2023 Jetpack Technologies Inc and contributors. All rights reserved.
// Use of this source code is governed by the license in the LICENSE file.

package boxcli

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"go.jetpack.io/devbox"
	"go.jetpack.io/devbox/internal/impl/devopt"
	"go.jetpack.io/devbox/internal/ux"
)

func globalCmd() *cobra.Command {

	globalCmd := &cobra.Command{}

	*globalCmd = cobra.Command{
		Use:                "global",
		Short:              "Manage global devbox packages",
		PersistentPreRunE:  setGlobalConfigForDelegatedCommands(globalCmd),
		PersistentPostRunE: ensureGlobalEnvEnabled,
	}

	addCommandAndHideConfigFlag(globalCmd, addCmd())
	addCommandAndHideConfigFlag(globalCmd, hookCmd())
	addCommandAndHideConfigFlag(globalCmd, installCmd())
	addCommandAndHideConfigFlag(globalCmd, pathCmd())
	addCommandAndHideConfigFlag(globalCmd, pullCmd())
	addCommandAndHideConfigFlag(globalCmd, pushCmd())
	addCommandAndHideConfigFlag(globalCmd, removeCmd())
	addCommandAndHideConfigFlag(globalCmd, runCmd())
	addCommandAndHideConfigFlag(globalCmd, servicesCmd())
	addCommandAndHideConfigFlag(globalCmd, shellEnvCmd())
	addCommandAndHideConfigFlag(globalCmd, updateCmd())

	// Create list for non-global? Mike: I want it :)
	globalCmd.AddCommand(globalListCmd())

	return globalCmd
}

func addCommandAndHideConfigFlag(parent *cobra.Command, child *cobra.Command) {
	parent.AddCommand(child)
	_ = child.Flags().MarkHidden("config")
}

func globalListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List global packages",
		PreRunE: ensureNixInstalled,
		RunE:    listGlobalCmdFunc,
	}
}

func listGlobalCmdFunc(cmd *cobra.Command, args []string) error {
	path, err := ensureGlobalConfig(cmd)
	if err != nil {
		return errors.WithStack(err)
	}

	box, err := devbox.Open(&devopt.Opts{
		Dir:    path,
		Writer: cmd.OutOrStdout(),
	})
	if err != nil {
		return errors.WithStack(err)
	}
	return box.PrintGlobalList()
}

var globalConfigPath string

func ensureGlobalConfig(cmd *cobra.Command) (string, error) {
	if globalConfigPath != "" {
		return globalConfigPath, nil
	}

	globalConfigPath, err := devbox.GlobalDataPath()
	if err != nil {
		return "", err
	}
	_, err = devbox.InitConfig(globalConfigPath, cmd.ErrOrStderr())
	if err != nil {
		return "", err
	}
	return globalConfigPath, nil
}

func setGlobalConfigForDelegatedCommands(
	globalCmd *cobra.Command,
) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		globalPath, err := ensureGlobalConfig(cmd)
		if err != nil {
			return err
		}

		for _, c := range globalCmd.Commands() {
			if f := c.Flag("config"); f != nil && f.Value.Type() == "string" {
				if err := f.Value.Set(globalPath); err != nil {
					return errors.WithStack(err)
				}
			}
		}
		return nil
	}
}

func ensureGlobalEnvEnabled(cmd *cobra.Command, args []string) error {
	// Skip checking this for shellenv and hook sub-commands of devbox global
	// since these commands are what will enable the global environment when
	// invoked from the user's shellrc
	if cmd.Name() == "shellenv" || cmd.Name() == "hook" {
		return nil
	}
	path, err := ensureGlobalConfig(cmd)
	if err != nil {
		return errors.WithStack(err)
	}

	box, err := devbox.Open(&devopt.Opts{
		Dir:    path,
		Writer: cmd.ErrOrStderr(),
	})
	if err != nil {
		return err
	}
	if !box.IsEnvEnabled() {
		fmt.Fprintln(cmd.ErrOrStderr())
		ux.Fwarning(
			cmd.ErrOrStderr(),
			`devbox global is not activated.

Add the following line to your shell's rcfile (e.g., ~/.bashrc or ~/.zshrc)
and restart your shell to fix this:

	eval "$(devbox global shellenv)"
`,
		)
	}
	return nil
}
