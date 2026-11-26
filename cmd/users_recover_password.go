package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/filebrowser/filebrowser/v2/users"
)

func init() {
	usersCmd.AddCommand(usersRecoverPasswordCmd)
}

var usersRecoverPasswordCmd = &cobra.Command{
	Use:   "recover-password <id|username>",
	Short: "Remove all security keys and set a new password (server stopped)",
	Args:  cobra.ExactArgs(1),
	RunE: withStore(func(cmd *cobra.Command, args []string, st *store) error {
		username, userID := parseUsernameOrID(args[0])
		var lookup any = username
		if userID != 0 {
			lookup = userID
		}
		u, err := st.Users.Get("", false, lookup)
		if err != nil {
			return err
		}
		if !u.KeyOnly {
			return errors.New("account is not enrolled for security-key login")
		}
		set, err := st.Settings.Get()
		if err != nil {
			return err
		}
		fd := int(os.Stdin.Fd())
		if !term.IsTerminal(fd) {
			return errors.New("an interactive terminal is required")
		}
		read := func(prompt string) ([]byte, error) {
			fmt.Fprint(cmd.ErrOrStderr(), prompt)
			value, err := term.ReadPassword(fd)
			fmt.Fprintln(cmd.ErrOrStderr())
			return value, err
		}
		first, err := read("New password: ")
		if err != nil {
			return err
		}
		defer clear(first)
		second, err := read("Confirm new password: ")
		if err != nil {
			return err
		}
		defer clear(second)
		if !bytes.Equal(first, second) {
			return errors.New("passwords do not match")
		}
		return recoverKeyAccount(st, u.ID, string(first), set.MinimumPasswordLength)
	}, storeOptions{}),
}

func recoverKeyAccount(st *store, id uint, newPassword string, minimum uint) error {
	state, err := st.SecurityKeys.Get(id)
	if err != nil {
		return err
	}
	if len(state.Keys) == 0 {
		return errors.New("account is not enrolled for security-key login")
	}
	hash, err := users.NewPasswordAfterKeyRemoval(newPassword, state.RetiredHash, minimum)
	if err != nil {
		return err
	}
	return st.SecurityKeys.Change(id, state.Epoch, func(user *users.User, security *users.SecurityState) error {
		user.Password = hash
		security.Keys = nil
		return nil
	})
}
