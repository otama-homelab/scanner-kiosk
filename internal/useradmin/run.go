// Package useradmin provisions simple-auth identities locally, without opening
// an HTTP registration endpoint or loading API/OIDC/worker credentials.
package useradmin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"scanner-kiosk/internal/authport"
	"scanner-kiosk/internal/config"
	"scanner-kiosk/internal/simpleauth"
	"scanner-kiosk/internal/storage"
)

var ErrUsage = errors.New("usage: scanner user create|set-password USERNAME (password from standard input)")

func Run(ctx context.Context, args []string, lookup config.Lookup, input io.Reader, output io.Writer) error {
	if len(args) != 2 || (args[0] != "create" && args[0] != "set-password") || lookup == nil || input == nil || output == nil {
		return ErrUsage
	}
	if _, err := simpleauth.NormalizeUsername(args[1]); err != nil {
		return err
	}
	root, ok := lookup("SCANNER_STATE_DIR")
	if !ok {
		root = "/var/lib/scanner/state"
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return authport.ErrUnavailable
	}
	info, err := os.Lstat(root)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return authport.ErrUnavailable
	}
	// A pipe/secret reader supplies one password; never accept it as an argument.
	// Remove only a single terminal line ending. Preserve spaces and Unicode.
	data, err := io.ReadAll(io.LimitReader(input, 1027))
	if err != nil {
		return authport.ErrUnavailable
	}
	defer clear(data)
	password := string(data)
	if strings.HasSuffix(password, "\r\n") {
		password = strings.TrimSuffix(password, "\r\n")
	} else {
		password = strings.TrimSuffix(password, "\n")
	}
	if len(data) > 1026 {
		return authport.ErrDenied
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	db, err := storage.Open(filepath.Join(root, "state.sqlite3"), storage.State)
	if err != nil {
		return authport.ErrUnavailable
	}
	defer db.Close()
	records, err := simpleauth.NewSQLiteRecords(db, time.Now)
	if err != nil {
		return err
	}
	service, err := simpleauth.New(records)
	if err != nil {
		return err
	}
	message := "User created."
	if args[0] == "create" {
		_, err = service.Create(ctx, args[1], password)
	} else {
		_, err = service.SetPassword(ctx, args[1], password)
		message = "Password updated."
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(output, message)
	return err
}
