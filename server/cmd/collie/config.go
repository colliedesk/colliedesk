package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/colliedesk/colliedesk/server/internal/database"
	"github.com/colliedesk/colliedesk/server/internal/serverconfig"
	"github.com/jackc/pgx/v5/pgxpool"
)

const configUsage = `usage: collie config <command>
  get [KEY]                         print the value of every setting, or of one (secrets: set or not)
  list [--group G]                  print every setting with where its value came from
  set KEY=VALUE... [--yes] [--by B] save settings as a new version (KEY: dotted name or variable name)
  unset KEY... [--by B]             remove settings (they go back to their default)
  reset KEY... [--by B]             same as unset
  set-secret KEY [--by B]           save a secret read from standard input, never from the command line
  import FILE|--from-env|--stdin [--only-missing|--replace] [--by B]
                                    import settings written as an .env file
  export [--format env|json] [--include-secrets]
  apply FILE [--dry-run] [--by B]   make the configuration match a JSON file (as export writes it)
  history                           list the versions
  restore VERSION [--by B]          save an older version again as the newest
  trial [confirm|revert] [--by B]   show the trial change that runs, keep it, or end it now
  doctor                            check the configuration; exit 0 clean, 1 warnings, 2 errors
  reseal [--by B]                   seal every secret again with the first key of MAILBOX_TOKEN_KEYS`

// exitCode makes the process exit with code; its message is printed as an error when not empty.
type exitCode struct {
	code    int
	message string
}

func (e exitCode) Error() string { return e.message }

func configCommand(ctx context.Context, pool *pgxpool.Pool, args []string, output io.Writer) error {
	if len(args) == 0 {
		return errors.New(configUsage)
	}
	store, err := openConfigStore(database.New(pool))
	if err != nil {
		return err
	}
	command, rest := args[0], args[1:]
	switch command {
	case "get":
		return configGet(ctx, store, rest, output)
	case "list":
		return configList(ctx, store, rest, output)
	case "set":
		return configSet(ctx, store, rest, output)
	case "unset", "reset":
		return configUnset(ctx, store, command, rest, output)
	case "set-secret":
		return configSetSecret(ctx, store, rest, output)
	case "import":
		return configImport(ctx, store, rest, output)
	case "export":
		return configExport(ctx, store, rest, output)
	case "apply":
		return configApply(ctx, store, rest, output)
	case "history":
		return configHistory(ctx, store, output)
	case "restore":
		return configRestore(ctx, store, rest, output)
	case "trial":
		return configTrial(ctx, store, rest, output)
	case "doctor":
		return configDoctor(ctx, store, output)
	case "reseal":
		return configReseal(ctx, store, rest, output)
	}
	return errors.New(configUsage)
}

// cliActor is who the audit log names for a command: --by, else the operating system user.
func cliActor(by string) (serverconfig.Actor, error) {
	name, err := operatorName(by)
	if err != nil {
		return serverconfig.Actor{}, err
	}
	return serverconfig.Actor{Kind: "cli", Name: "cli:" + name}, nil
}

func configFlags(name string) (*flag.FlagSet, *string) {
	flags := flag.NewFlagSet("config "+name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	by := flags.String("by", "", "who makes the change, for the audit log")
	return flags, by
}

// parseFlagsAnywhere lets flags follow positional arguments (collie config set A=1 --yes).
func parseFlagsAnywhere(flags *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for len(args) > 0 {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		args = flags.Args()
		if len(args) > 0 {
			positional = append(positional, args[0])
			args = args[1:]
		}
	}
	return positional, nil
}

func secretState(loaded serverconfig.Loaded, key serverconfig.Key) string {
	if slices.Contains(loaded.Unopened, key.Name) {
		return "(sealed with a key the ring no longer has)"
	}
	if _, ok := loaded.Get(key.Name); ok {
		if !loaded.Ring() {
			return "(set in the environment)"
		}
		return "(set)"
	}
	return "(not set)"
}

func configGet(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	if len(args) > 1 {
		return errors.New("usage: collie config get [KEY]")
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		return err
	}
	for _, key := range serverconfig.Keys() {
		if len(args) == 1 {
			found, ok := serverconfig.Lookup(args[0])
			if !ok {
				return fmt.Errorf("%w: %s", serverconfig.ErrUnknownKey, args[0])
			}
			if found.Name != key.Name {
				continue
			}
		}
		value, _ := loaded.Get(key.Name)
		if key.Secret {
			value = secretState(loaded, key)
		}
		if _, err := fmt.Fprintf(output, "%s=%s\n", key.Name, value); err != nil {
			return err
		}
	}
	return nil
}

func configList(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("config list", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	group := flags.String("group", "", "only this console group")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 {
		return errors.New("usage: collie config list [--group ai|email|mailboxes|billing|signups|security|limits|operator]")
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "running version %d\n", loaded.Version); err != nil {
		return err
	}
	for _, key := range serverconfig.Keys() {
		if *group != "" && key.Group != *group {
			continue
		}
		value, set := loaded.Get(key.Name)
		if key.Secret {
			value = secretState(loaded, key)
		}
		source := "default"
		if meta, ok := loaded.Meta[key.Name]; ok {
			source = fmt.Sprintf("v%d %s by %s", meta.Version, meta.Source, meta.By)
		} else if set {
			source = "environment"
		} else if key.Default != "" {
			value = key.Default
		}
		if _, err := fmt.Fprintf(output, "%-34s %-8s %s\t%s\n", key.Name, key.Group, value, source); err != nil {
			return err
		}
	}
	products, err := store.Products(ctx)
	if err != nil {
		return err
	}
	for _, product := range products {
		if *group != "" && *group != serverconfig.GroupBilling {
			break
		}
		state := "off sale"
		if product.OnSale {
			state = "on sale"
		}
		if _, err := fmt.Fprintf(output, "product %s %s %s %s\n", product.ProductID, product.Plan, product.Period, state); err != nil {
			return err
		}
	}
	return nil
}

// guardedWithoutYes lists the keys of a change whose console guard shows a consequence or asks to type a
// phrase: on the command line, --yes stands for both.
func guardedWithoutYes(names []string) []string {
	var guarded []string
	for _, name := range names {
		key, _ := serverconfig.Lookup(name)
		if slices.Contains(key.Guards, serverconfig.GuardPreview) || slices.Contains(key.Guards, serverconfig.GuardTypeToConfirm) {
			guarded = append(guarded, key.Name)
		}
	}
	return guarded
}

// saveChecked checks the configuration a change makes, the way serve builds it, then saves it.
func saveChecked(ctx context.Context, store *serverconfig.Store, change serverconfig.Change) (int64, error) {
	loaded, err := store.Current(ctx)
	if err != nil {
		return 0, err
	}
	next := loaded.With(change.Set, change.Unset)
	if _, err := checkSnapshot(next); err != nil {
		return 0, fmt.Errorf("not saved: %w", err)
	}
	if change.Set["operator.require_totp"] == "false" && hostedCloud(next.Getenv) {
		return 0, errors.New("not saved: the hosted cloud always requires an authenticator for operators; to replace a lost one run collie operator pair --reset-totp")
	}
	change.Base = loaded.Version
	return store.Save(ctx, change)
}

func configSet(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags, by := configFlags("set")
	yes := flags.Bool("yes", false, "confirm a change the console would ask you to confirm")
	pairs, err := parseFlagsAnywhere(flags, args)
	if err != nil || len(pairs) == 0 {
		return errors.New("usage: collie config set KEY=VALUE... [--yes] [--by NAME]")
	}
	set := map[string]string{}
	for _, pair := range pairs {
		name, value, ok := strings.Cut(pair, "=")
		key, known := serverconfig.Lookup(name)
		switch {
		case !ok:
			return fmt.Errorf("write %s as KEY=VALUE", pair)
		case !known:
			return fmt.Errorf("%w: %s", serverconfig.ErrUnknownKey, name)
		case key.Secret:
			return fmt.Errorf("%s is a secret: use collie config set-secret %s and type it on standard input, so it never sits in your shell history", key.Name, key.Name)
		}
		set[key.Name] = strings.TrimSpace(value)
	}
	if guarded := guardedWithoutYes(slices.Collect(maps.Keys(set))); len(guarded) > 0 && !*yes {
		return fmt.Errorf("%s changes what the server does for every workspace; read docs/self-host/operator-console.md, then add --yes", strings.Join(guarded, ", "))
	}
	actor, err := cliActor(*by)
	if err != nil {
		return err
	}
	version, err := saveChecked(ctx, store, serverconfig.Change{Set: set, Source: "cli", Actor: actor})
	if err != nil {
		return err
	}
	return reportSaved(output, version)
}

func reportSaved(output io.Writer, version int64) error {
	_, err := fmt.Fprintf(output, "saved version %d; a running server applies it within a few seconds (restart it to apply it now)\n", version)
	return err
}

func configUnset(ctx context.Context, store *serverconfig.Store, command string, args []string, output io.Writer) error {
	flags, by := configFlags(command)
	names, err := parseFlagsAnywhere(flags, args)
	if err != nil || len(names) == 0 {
		return fmt.Errorf("usage: collie config %s KEY... [--by NAME]", command)
	}
	var unset []string
	for _, name := range names {
		key, ok := serverconfig.Lookup(name)
		if !ok {
			return fmt.Errorf("%w: %s", serverconfig.ErrUnknownKey, name)
		}
		unset = append(unset, key.Name)
	}
	actor, err := cliActor(*by)
	if err != nil {
		return err
	}
	version, err := saveChecked(ctx, store, serverconfig.Change{Unset: unset, Source: "cli", Actor: actor})
	if err != nil {
		return err
	}
	return reportSaved(output, version)
}

func configSetSecret(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags, by := configFlags("set-secret")
	names, err := parseFlagsAnywhere(flags, args)
	if err != nil || len(names) != 1 {
		return errors.New("usage: collie config set-secret KEY [--by NAME] < file (the secret is read from standard input)")
	}
	key, ok := serverconfig.Lookup(names[0])
	if !ok || !key.Secret {
		return fmt.Errorf("%s is not a secret setting", names[0])
	}
	if !store.Keys().Configured() {
		return fmt.Errorf("%s cannot be stored: set MAILBOX_TOKEN_KEYS first (docs/self-host/operator-console.md); until then it is read from the environment", key.Name)
	}
	raw, err := io.ReadAll(io.LimitReader(stdin, 64<<10))
	if err != nil {
		return err
	}
	value := strings.TrimRight(string(raw), "\r\n")
	if strings.TrimSpace(value) == "" {
		return errors.New("standard input is empty; nothing saved")
	}
	actor, err := cliActor(*by)
	if err != nil {
		return err
	}
	version, err := saveChecked(ctx, store, serverconfig.Change{Set: map[string]string{key.Name: value}, Source: "cli", Actor: actor})
	if err != nil {
		return err
	}
	return reportSaved(output, version)
}

// parseEnvFile reads KEY=VALUE lines as compose and sh read .env: # comments, blank lines, optional
// export, values in single or double quotes.
func parseEnvFile(reader io.Reader) (map[string]string, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64<<10), 1<<20)
	line := 0
	for scanner.Scan() {
		line++
		text := strings.TrimSpace(scanner.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		text = strings.TrimPrefix(text, "export ")
		name, value, ok := strings.Cut(text, "=")
		if !ok {
			return nil, fmt.Errorf("line %d is not KEY=VALUE", line)
		}
		value = strings.TrimSpace(value)
		if len(value) >= 2 && (value[0] == '"' && value[len(value)-1] == '"' || value[0] == '\'' && value[len(value)-1] == '\'') {
			if value[0] == '"' {
				unquoted, err := strconv.Unquote(value)
				if err == nil {
					value = unquoted
				} else {
					value = value[1 : len(value)-1]
				}
			} else {
				value = value[1 : len(value)-1]
			}
		}
		values[strings.TrimSpace(name)] = value
	}
	return values, scanner.Err()
}

func configImport(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags, by := configFlags("import")
	fromEnv := flags.Bool("from-env", false, "import from this process's environment")
	fromStdin := flags.Bool("stdin", false, "read the .env lines from standard input")
	onlyMissing := flags.Bool("only-missing", false, "import only the groups the database does not hold (the default)")
	replace := flags.Bool("replace", false, "replace the groups the file sets, even when the database holds them")
	files, err := parseFlagsAnywhere(flags, args)
	sources := len(files)
	if *fromEnv {
		sources++
	}
	if *fromStdin {
		sources++
	}
	if err != nil || sources != 1 || len(files) > 1 || (*onlyMissing && *replace) {
		return errors.New("usage: collie config import FILE|--from-env|--stdin [--only-missing|--replace] [--by NAME]")
	}
	getenv := os.Getenv
	// Only the server's own environment may lead to a generated sign-up hash key: a file without the key
	// says nothing about it, and the environment of the server that reads it may still hold one.
	skipSignupKey := false
	if !*fromEnv {
		reader := stdin
		if len(files) == 1 {
			file, err := os.Open(files[0])
			if err != nil {
				return err
			}
			defer file.Close()
			reader = file
		}
		values, err := parseEnvFile(reader)
		if err != nil {
			return err
		}
		for name := range values {
			if !importable(name) {
				_, _ = fmt.Fprintf(stderr, "skipping %s: it is not a setting the server keeps in the database\n", name)
			}
		}
		getenv = func(name string) string { return values[name] }
		_, mentioned := values[serverconfig.SignupHashKeyEnv]
		skipSignupKey = !mentioned
	}
	actor, err := cliActor(*by)
	if err != nil {
		return err
	}
	result, err := store.ImportEnv(ctx, getenv, importGroupCheck, actor, serverconfig.ImportOptions{Replace: *replace, SkipSignupKey: skipSignupKey,
		Hosted: func(snap serverconfig.Snapshot) bool { return hostedCloud(snap.Getenv) }})
	if err != nil {
		return err
	}
	return printImport(output, result)
}

// importable says whether an .env line is a setting the database keeps (bootstrap and installer lines are
// expected in a whole .env and skipped quietly).
func importable(name string) bool {
	if slices.Contains(serverconfig.MovedEnvs(), name) {
		return true
	}
	if _, ok := serverconfig.Lookup(name); ok {
		return true
	}
	quiet := append(slices.Clone(serverconfig.Bootstrap), serverconfig.TelemetryOffEnvs...)
	quiet = append(quiet, serverconfig.DeadEnvs...)
	quiet = append(quiet, "POSTGRES_USER", "POSTGRES_PASSWORD", "COLLIEDESK_OWNER_PASSWORD", "COLLIE_DESK_IMAGE", "COLLIE_DESK_PORT")
	return slices.Contains(quiet, name)
}

func printImport(output io.Writer, result serverconfig.ImportResult) error {
	lines := []string{}
	for _, group := range result.Imported {
		lines = append(lines, "imported "+group)
	}
	for _, group := range result.Skipped {
		lines = append(lines, "kept "+group+" (the database already holds it)")
	}
	groups := slices.Sorted(maps.Keys(result.Invalid))
	for _, group := range groups {
		lines = append(lines, "not imported "+group+": "+result.Invalid[group])
	}
	for _, key := range result.Secrets {
		lines = append(lines, "imported secret "+key)
	}
	if result.SignupKey != "" && result.SignupKey != "kept" {
		lines = append(lines, "sign-up hash key "+result.SignupKey)
	}
	if len(lines) == 0 {
		lines = append(lines, "nothing to import")
	}
	if _, err := fmt.Fprintln(output, strings.Join(lines, "\n")); err != nil {
		return err
	}
	if len(result.Invalid) > 0 {
		return exitCode{code: 1, message: "some groups were not imported"}
	}
	return nil
}

// exportFile is the JSON export: open, documented in docs/self-host/operator-console.md, without secrets
// unless --include-secrets.
type exportFile struct {
	Format   string                    `json:"format"`
	Version  int64                     `json:"version"`
	Settings map[string]string         `json:"settings"`
	Secrets  map[string]string         `json:"secrets,omitempty"`
	Products []serverconfig.ProductRow `json:"products"`
}

func configExport(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags := flag.NewFlagSet("config export", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	format := flags.String("format", "env", "env or json")
	includeSecrets := flags.Bool("include-secrets", false, "also write the secrets, in plain text")
	if err := flags.Parse(args); err != nil || flags.NArg() != 0 || (*format != "env" && *format != "json") {
		return errors.New("usage: collie config export [--format env|json] [--include-secrets]")
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		return err
	}
	if *includeSecrets {
		_, _ = fmt.Fprintln(stderr, "warning: this export holds the server's secrets in plain text; keep it like a password and delete it after use")
	}
	file := exportFile{Format: "colliedesk-config/1", Version: loaded.Version, Settings: map[string]string{}, Products: loaded.Products()}
	if file.Products == nil {
		file.Products = []serverconfig.ProductRow{}
	}
	for _, key := range serverconfig.Keys() {
		value, ok := loaded.Get(key.Name)
		if !ok {
			continue
		}
		if key.Secret {
			if *includeSecrets {
				if file.Secrets == nil {
					file.Secrets = map[string]string{}
				}
				file.Secrets[key.Name] = value
			}
			continue
		}
		file.Settings[key.Name] = value
	}
	if *format == "json" {
		encoder := json.NewEncoder(output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(file)
	}
	// The env format is what an older release reads (docs/self-host/upgrade.md › Going back).
	lines := []string{fmt.Sprintf("# colliedesk configuration version %d", loaded.Version)}
	for _, key := range serverconfig.Keys() {
		if key.Env == "" {
			continue
		}
		value, ok := file.Settings[key.Name]
		if key.Secret {
			value, ok = file.Secrets[key.Name]
		}
		if ok {
			lines = append(lines, key.Env+"="+quoteEnv(value))
		}
	}
	for _, product := range serverconfig.ProductEnvs {
		if id := loaded.Getenv(product.Env); id != "" {
			lines = append(lines, product.Env+"="+quoteEnv(id))
		}
	}
	if *includeSecrets {
		if key := loaded.Getenv(serverconfig.SignupHashKeyEnv); key != "" {
			lines = append(lines, serverconfig.SignupHashKeyEnv+"="+quoteEnv(key))
		}
	}
	_, err = fmt.Fprintln(output, strings.Join(lines, "\n"))
	return err
}

// quoteEnv writes a value .env and sh read back unchanged.
func quoteEnv(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\n\"'#$\\`") {
		return strconv.Quote(value)
	}
	return value
}

func configApply(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags, by := configFlags("apply")
	dryRun := flags.Bool("dry-run", false, "print what would change; save nothing")
	files, err := parseFlagsAnywhere(flags, args)
	if err != nil || len(files) != 1 {
		return errors.New("usage: collie config apply FILE [--dry-run] [--by NAME]")
	}
	raw, err := os.ReadFile(files[0])
	if err != nil {
		return err
	}
	var file exportFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("%s is not a configuration file: %w", files[0], err)
	}
	loaded, err := store.Current(ctx)
	if err != nil {
		return err
	}
	set, unset := map[string]string{}, []string{}
	var diff []string
	for _, key := range serverconfig.Keys() {
		want, inFile := file.Settings[key.Name]
		if key.Secret {
			// A file without the secret keeps it: exports leave secrets out.
			if secret, ok := file.Secrets[key.Name]; ok {
				if current, _ := loaded.Get(key.Name); current != secret {
					set[key.Name] = secret
					diff = append(diff, "~ "+key.Name+" (secret)")
				}
			}
			continue
		}
		current, isSet := loaded.Get(key.Name)
		switch {
		case inFile && (!isSet || current != want):
			set[key.Name] = want
			diff = append(diff, fmt.Sprintf("~ %s=%s", key.Name, want))
		case !inFile && isSet:
			unset = append(unset, key.Name)
			diff = append(diff, "- "+key.Name)
		}
	}
	for name := range file.Settings {
		if _, ok := serverconfig.Lookup(name); !ok {
			return fmt.Errorf("%w in %s: %s", serverconfig.ErrUnknownKey, files[0], name)
		}
	}
	sort.Strings(diff)
	if len(diff) == 0 {
		_, err := fmt.Fprintln(output, "the configuration already matches; nothing to save")
		return err
	}
	if _, err := fmt.Fprintln(output, strings.Join(diff, "\n")); err != nil {
		return err
	}
	if *dryRun {
		return nil
	}
	actor, err := cliActor(*by)
	if err != nil {
		return err
	}
	version, err := saveChecked(ctx, store, serverconfig.Change{Set: set, Unset: unset, Source: "apply-file", Actor: actor, Note: "applied " + files[0]})
	if err != nil {
		return err
	}
	return reportSaved(output, version)
}

func configHistory(ctx context.Context, store *serverconfig.Store, output io.Writer) error {
	versions, err := store.History(ctx, 100)
	if err != nil {
		return err
	}
	for _, version := range versions {
		if _, err := fmt.Fprintf(output, "v%d\t%s\t%s\t%s\t%s\t%s\n", version.ID, version.CreatedAt.UTC().Format("2006-01-02T15:04:05Z"), version.Source,
			version.CreatedBy, version.Status, strings.Join(version.Changed, ",")); err != nil {
			return err
		}
	}
	return nil
}

func configRestore(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags, by := configFlags("restore")
	positional, err := parseFlagsAnywhere(flags, args)
	if err != nil || len(positional) != 1 {
		return errors.New("usage: collie config restore VERSION [--by NAME]")
	}
	from, err := strconv.ParseInt(strings.TrimPrefix(positional[0], "v"), 10, 64)
	if err != nil {
		return errors.New("VERSION is a number, as collie config history prints it")
	}
	old, err := store.Load(ctx, from)
	if err != nil {
		return err
	}
	if _, err := checkSnapshot(old.Snapshot); err != nil {
		return fmt.Errorf("not restored: version %d does not make a valid setting: %w", from, err)
	}
	actor, err := cliActor(*by)
	if err != nil {
		return err
	}
	version, err := store.Restore(ctx, from, 0, actor, "")
	if err != nil {
		return err
	}
	return reportSaved(output, version)
}

// configTrial shows the trial change that runs (D12), keeps it, or reverts it now.
func configTrial(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags, by := configFlags("trial")
	positional, err := parseFlagsAnywhere(flags, args)
	if err != nil || len(positional) > 1 {
		return errors.New("usage: collie config trial [confirm|revert] [--by NAME]")
	}
	run, ok, err := store.Trial(ctx)
	if err != nil {
		return err
	}
	if !ok {
		_, err := fmt.Fprintln(output, "no trial change runs")
		return err
	}
	if len(positional) == 0 {
		_, err := fmt.Fprintf(output, "v%d is tried until %s (by %s; %s); it reverts to v%d unless confirmed\n",
			run.Version, run.ExpiresAt.UTC().Format(time.RFC3339), run.StartedBy, strings.Join(run.Keys, ", "), run.Parent)
		return err
	}
	actor, err := cliActor(*by)
	if err != nil {
		return err
	}
	switch positional[0] {
	case "confirm":
		if err := store.ConfirmTrial(ctx, run.Version, actor); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "v%d is kept\n", run.Version)
		return err
	case "revert":
		version, err := store.RevertTrial(ctx, run.Version, actor)
		if err != nil {
			return err
		}
		return reportSaved(output, version)
	}
	return errors.New("usage: collie config trial [confirm|revert] [--by NAME]")
}

// doctorFindings checks the configuration: warnings can wait, errors need a person now.
func doctorFindings(ctx context.Context, store *serverconfig.Store, environ func(string) string) (warnings, errs []string, err error) {
	loaded, err := store.Current(ctx)
	if err != nil {
		return nil, nil, err
	}
	for _, leftover := range serverconfig.Leftovers(loaded.Snapshot, environ) {
		if leftover.Same {
			warnings = append(warnings, fmt.Sprintf("%s is still set in the environment and equals %s in the database: remove it from .env and compose.yaml", leftover.Env, leftover.Key))
		} else {
			warnings = append(warnings, fmt.Sprintf("%s is set in the environment, differs from %s in the database and is ignored", leftover.Env, leftover.Key))
		}
	}
	for _, name := range serverconfig.Dead(environ) {
		warnings = append(warnings, name+" is set but no release reads it: remove it")
	}
	if run, ok, err := store.Trial(ctx); err == nil && ok {
		warnings = append(warnings, fmt.Sprintf("v%d is a trial change until %s: confirm or revert it (collie config trial confirm|revert)", run.Version, run.ExpiresAt.UTC().Format(time.RFC3339)))
	}
	for _, key := range loaded.Unopened {
		warnings = append(warnings, key+" is sealed with a key MAILBOX_TOKEN_KEYS no longer has: set it again")
	}
	if !store.Keys().Configured() {
		warnings = append(warnings, "MAILBOX_TOKEN_KEYS is not set: secrets stay in the environment until you add a key ring")
	} else {
		for key, meta := range loaded.Meta {
			if meta.KeyID != "" && meta.KeyID != store.Keys().Current() && !slices.Contains(loaded.Unopened, key) {
				warnings = append(warnings, key+" is sealed with an older key: run collie config reseal")
				break
			}
		}
	}
	history, err := store.History(ctx, 20)
	if err != nil {
		return nil, nil, err
	}
	for _, version := range history {
		if version.ID == loaded.Version && version.Status == "rejected" {
			errs = append(errs, fmt.Sprintf("version %d is refused by the server: %s", version.ID, version.Rejected))
		}
	}
	if loaded.Version != 0 {
		if _, buildErr := checkSnapshot(loaded.Snapshot); buildErr != nil {
			errs = append(errs, fmt.Sprintf("version %d does not make a valid setting: %v", loaded.Version, buildErr))
		}
	}
	sort.Strings(warnings)
	return warnings, errs, nil
}

func configDoctor(ctx context.Context, store *serverconfig.Store, output io.Writer) error {
	warnings, errs, err := doctorFindings(ctx, store, os.Getenv)
	if err != nil {
		return err
	}
	for _, line := range errs {
		if _, err := fmt.Fprintln(output, "error: "+line); err != nil {
			return err
		}
	}
	for _, line := range warnings {
		if _, err := fmt.Fprintln(output, "warning: "+line); err != nil {
			return err
		}
	}
	switch {
	case len(errs) > 0:
		return exitCode{code: 2}
	case len(warnings) > 0:
		return exitCode{code: 1}
	}
	_, err = fmt.Fprintln(output, "the configuration is clean")
	return err
}

func configReseal(ctx context.Context, store *serverconfig.Store, args []string, output io.Writer) error {
	flags, by := configFlags("reseal")
	if positional, err := parseFlagsAnywhere(flags, args); err != nil || len(positional) != 0 {
		return errors.New("usage: collie config reseal [--by NAME]")
	}
	if !store.Keys().Configured() {
		return errors.New("MAILBOX_TOKEN_KEYS is not set: there is no key to seal with")
	}
	actor, err := cliActor(*by)
	if err != nil {
		return err
	}
	version, keys, err := store.Reseal(ctx, actor)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		_, err := fmt.Fprintf(output, "every secret is sealed with %s already\n", store.Keys().Current())
		return err
	}
	_, err = fmt.Fprintf(output, "sealed %s again with %s as version %d\n", strings.Join(keys, ", "), store.Keys().Current(), version)
	return err
}
