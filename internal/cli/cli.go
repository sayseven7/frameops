// Package cli implements the fops commands that work through the FrameOPS HTTP
// API. It deliberately imports no database or object-storage package: the API is
// the only entry point the CLI has, and the sole direct-database exception, the
// local first-admin bootstrap, lives outside this package.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const usage = `usage:
  fops login --api URL --email EMAIL --password-file PATH
  fops ingest nmap FILE --engagement ENGAGEMENT_ID`

// Run executes one command and returns the process exit status: 0 on success, 1
// on a failed operation, 2 on a usage error.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		say(stderr, "%s\n", usage)
		return 2
	}
	var err error
	switch args[0] {
	case "login":
		err = login(args[1:], stdout)
	case "ingest":
		err = ingest(args[1:], stdout)
	default:
		say(stderr, "%s\n", usage)
		return 2
	}
	if errors.Is(err, errUsage) {
		say(stderr, "%v\n%s\n", err, usage)
		return 2
	}
	if err != nil {
		say(stderr, "%v\n", err)
		return 1
	}
	return 0
}

// say writes one operator-facing line. The CLI reports what it did on a writer
// the caller owns, and a closed output stream is not a reason to fail an
// operation the API already accepted.
func say(writer io.Writer, format string, arguments ...any) {
	_, _ = fmt.Fprintf(writer, format, arguments...)
}

var errUsage = errors.New("usage")

// login exchanges the operator's credentials for the session the API issues and
// stores only that session locally. The password is read from a file and is
// never written anywhere by the CLI.
func login(args []string, stdout io.Writer) error {
	flags := flag.NewFlagSet("login", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	api := flags.String("api", "", "base URL of the FrameOPS API")
	email := flags.String("email", "", "operator email")
	passwordFile := flags.String("password-file", "", "file containing the operator password")
	if err := flags.Parse(args); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if flags.NArg() != 0 || *api == "" || *email == "" || *passwordFile == "" {
		return fmt.Errorf("%w: login requires --api, --email and --password-file", errUsage)
	}
	base, err := baseURL(*api)
	if err != nil {
		return err
	}
	password, err := os.ReadFile(*passwordFile)
	if err != nil {
		return fmt.Errorf("read password file: %w", err)
	}
	session, err := authenticate(base, *email, strings.TrimRight(string(password), "\r\n"))
	if err != nil {
		return err
	}
	path, err := storeSession(storedSession{API: base, Session: session})
	if err != nil {
		return err
	}
	say(stdout, "signed in to %s; session stored in %s\n", base, path)
	return nil
}

// ingest uploads one tool artifact exactly as the tool wrote it. The CLI does
// not parse or summarize the artifact: the API is what reads it, so every client
// imports a scan under the same rules. The MVP CLI is online-only, so a failed
// upload is reported and repeated rather than queued.
func ingest(args []string, stdout io.Writer) error {
	if len(args) == 0 || args[0] != "nmap" {
		return fmt.Errorf("%w: the only supported ingestion is `ingest nmap`", errUsage)
	}
	flags := flag.NewFlagSet("ingest nmap", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	engagement := flags.String("engagement", "", "engagement identifier that receives the imported assets")
	// The artifact is named where the tool left it in the operator's command
	// line, before or after the flags, so `ingest nmap ./scan.xml --engagement X`
	// and the reverse order both work.
	files, options := splitArguments(args[1:])
	if err := flags.Parse(options); err != nil {
		return fmt.Errorf("%w: %v", errUsage, err)
	}
	if len(files) != 1 || flags.NArg() != 0 || *engagement == "" {
		return fmt.Errorf("%w: ingest nmap requires one FILE and --engagement", errUsage)
	}
	artifact, err := os.ReadFile(files[0])
	if err != nil {
		return fmt.Errorf("read artifact: %w", err)
	}
	stored, err := loadSession()
	if err != nil {
		return err
	}
	recorded, err := uploadIngestion(stored, *engagement, filepath.Base(files[0]), artifact)
	if err != nil {
		return err
	}
	say(stdout, "ingested %s artifact %s\n", recorded.Tool, recorded.Filename)
	say(stdout, "  ingestion %s\n", recorded.ID)
	say(stdout, "  format    %s\n", recorded.FormatVersion)
	say(stdout, "  sha256    %s\n", recorded.SHA256)
	say(stdout, "  hosts     read %d created %d reused %d ignored %d rejected %d\n",
		recorded.Summary.Read, recorded.Summary.Created, recorded.Summary.Reused, recorded.Summary.Ignored, recorded.Summary.Rejected)
	return nil
}

// splitArguments separates the file names of a command from its options, so the
// order an operator types them in does not change what the command does. Every
// option this CLI defines takes a value, and `--` ends the options.
func splitArguments(args []string) (files, options []string) {
	for index := 0; index < len(args); index++ {
		argument := args[index]
		switch {
		case argument == "--":
			return append(files, args[index+1:]...), options
		case strings.HasPrefix(argument, "-"):
			options = append(options, argument)
			if !strings.Contains(argument, "=") && index+1 < len(args) {
				index++
				options = append(options, args[index])
			}
		default:
			files = append(files, argument)
		}
	}
	return files, options
}
