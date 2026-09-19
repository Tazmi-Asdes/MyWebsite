package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
	"mywebsite/internal/app"
	"mywebsite/internal/auth"
	"mywebsite/internal/database"
	"mywebsite/internal/database/dbgen"
)

const databaseOperationTimeout = 10 * time.Second

// Database is the narrow command-layer view of database.Database. It makes
// command dispatch testable without opening a real MySQL connection.
type Database interface {
	CheckReady(context.Context) error
	Queries() *dbgen.Queries
	Close() error
}

// CommandDeps contains the injectable edges of adminctl. Production defaults
// use os.Stdin/os.Stdout/os.Stderr, term.ReadPassword, app.LoadConfig, and
// database.Open.
type CommandDeps struct {
	In           io.Reader
	Out          io.Writer
	Err          io.Writer
	IsTerminal   func() bool
	ReadPassword func() ([]byte, error)
	LoadConfig   func() (app.Config, error)
	OpenDatabase func(context.Context, database.Config) (Database, error)
	CreateAdmin  func(context.Context, string, string) error
}

// Run dispatches one adminctl invocation and returns its process exit code.
// It accepts no password-bearing command-line arguments.
func Run(args []string, dependencies ...CommandDeps) int {
	deps := defaultCommandDeps()
	if len(dependencies) > 0 {
		deps = mergeCommandDeps(deps, dependencies[0])
	}
	return dispatch(args, deps)
}

// run is kept as a small test-friendly alias for callers in package main.
func run(args []string, dependencies ...CommandDeps) int {
	return Run(args, dependencies...)
}

func main() {
	os.Exit(Run(os.Args[1:]))
}

func dispatch(args []string, deps CommandDeps) int {
	if len(args) == 1 && args[0] == "version" {
		fmt.Fprintln(deps.Out, "dev")
		return 0
	}
	if len(args) == 2 && args[0] == "admin" && args[1] == "create" {
		return createAdminCommand(deps)
	}
	fmt.Fprintln(deps.Err, "用法: adminctl version | adminctl admin create")
	return 2
}

func createAdminCommand(deps CommandDeps) int {
	if !deps.IsTerminal() {
		fmt.Fprintln(deps.Err, "admin create requires an interactive terminal")
		return 2
	}

	username, err := promptLine(deps.Out, "Username: ", deps.In)
	if err != nil {
		fmt.Fprintln(deps.Err, "failed to read username")
		return 2
	}
	fmt.Fprintln(deps.Out)

	fmt.Fprint(deps.Out, "Password: ")
	password, err := deps.ReadPassword()
	if err != nil {
		fmt.Fprintln(deps.Err, "failed to read password")
		return 2
	}
	fmt.Fprintln(deps.Out)
	fmt.Fprint(deps.Out, "Confirm password: ")
	confirmation, err := deps.ReadPassword()
	if err != nil {
		fmt.Fprintln(deps.Err, "failed to read password confirmation")
		return 2
	}
	fmt.Fprintln(deps.Out)
	if !sameBytes(password, confirmation) {
		fmt.Fprintln(deps.Err, "passwords do not match")
		return 2
	}

	config, err := deps.LoadConfig()
	if err != nil {
		fmt.Fprintln(deps.Err, "configuration unavailable")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), databaseOperationTimeout)
	defer cancel()
	db, err := deps.OpenDatabase(ctx, config.Database)
	if err != nil {
		fmt.Fprintln(deps.Err, "database unavailable")
		return 1
	}
	defer db.Close()
	if err := db.CheckReady(ctx); err != nil {
		fmt.Fprintln(deps.Err, "database unavailable")
		return 1
	}

	createAdmin := deps.CreateAdmin
	if createAdmin == nil {
		service := auth.NewService(db.Queries())
		createAdmin = service.CreateAdmin
	}
	if err := createAdmin(ctx, username, string(password)); err != nil {
		fmt.Fprintln(deps.Err, adminCreateError(err))
		return 1
	}
	fmt.Fprintln(deps.Out, "admin created successfully")
	return 0
}

func promptLine(out io.Writer, prompt string, in io.Reader) (string, error) {
	fmt.Fprint(out, prompt)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if errors.Is(err, io.EOF) && len(line) == 0 {
		return "", io.EOF
	}
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line, nil
}

func adminCreateError(err error) string {
	switch {
	case errors.Is(err, auth.ErrAdminAlreadyExists):
		return "admin already exists"
	case errors.Is(err, auth.ErrInvalidUsername):
		return "invalid username"
	case errors.Is(err, auth.ErrInvalidPassword),
		errors.Is(err, auth.ErrPasswordTooShort),
		errors.Is(err, auth.ErrPasswordTooLong),
		errors.Is(err, auth.ErrPasswordWhitespace),
		errors.Is(err, auth.ErrPasswordBytes):
		return "invalid password"
	case errors.Is(err, auth.ErrStore):
		return "database operation failed"
	default:
		return "admin creation failed"
	}
}

func sameBytes(first, second []byte) bool {
	if len(first) != len(second) {
		return false
	}
	var different byte
	for index := range first {
		different |= first[index] ^ second[index]
	}
	return different == 0
}

func defaultCommandDeps() CommandDeps {
	stdin := os.Stdin
	return CommandDeps{
		In:  stdin,
		Out: os.Stdout,
		Err: os.Stderr,
		IsTerminal: func() bool {
			fd := int(stdin.Fd())
			return term.IsTerminal(fd)
		},
		ReadPassword: func() ([]byte, error) {
			return term.ReadPassword(int(stdin.Fd()))
		},
		LoadConfig: app.LoadConfig,
		OpenDatabase: func(ctx context.Context, config database.Config) (Database, error) {
			return database.Open(ctx, config)
		},
	}
}

func mergeCommandDeps(defaults, overrides CommandDeps) CommandDeps {
	if overrides.In != nil {
		defaults.In = overrides.In
	}
	if overrides.Out != nil {
		defaults.Out = overrides.Out
	}
	if overrides.Err != nil {
		defaults.Err = overrides.Err
	}
	if overrides.IsTerminal != nil {
		defaults.IsTerminal = overrides.IsTerminal
	}
	if overrides.ReadPassword != nil {
		defaults.ReadPassword = overrides.ReadPassword
	}
	if overrides.LoadConfig != nil {
		defaults.LoadConfig = overrides.LoadConfig
	}
	if overrides.OpenDatabase != nil {
		defaults.OpenDatabase = overrides.OpenDatabase
	}
	if overrides.CreateAdmin != nil {
		defaults.CreateAdmin = overrides.CreateAdmin
	}
	return defaults
}
