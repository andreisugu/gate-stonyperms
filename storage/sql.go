package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/google/uuid"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"github.com/andreisugu/gate-stonyperms/model"
)

// Dialect identifies the backing SQL database engine.
type Dialect string

const (
	DialectSQLite     Dialect = "sqlite"
	DialectMySQL      Dialect = "mysql"
	DialectMariaDB    Dialect = "mariadb"
	DialectPostgreSQL Dialect = "postgresql"

	SchemaVersion = 1
)

var tablePrefixRegex = regexp.MustCompile(`^[a-zA-Z0-9_]{0,32}$`)

// SQLConfig configures an SQLStorage provider across SQLite, MySQL/MariaDB, and PostgreSQL.
type SQLConfig struct {
	Type                   string
	File                   string
	Host                   string
	Port                   int
	Database               string
	Username               string
	Password               string
	PasswordFile           string
	TablePrefix            string
	SSLMode                string
	MaxOpenConns           int
	MaxIdleConns           int
	ConnMaxLifetimeSeconds int
	ConnectTimeoutSeconds  int
	StorageDir             string
}

// SQLStorage persists StonyPerms entities using the canonical v1 relational schema.
type SQLStorage struct {
	db         *sql.DB
	dialect    Dialect
	prefix     string
	storageDir string
	target     string
	writeMu    sync.Mutex
}

// NormalizeStorageType normalizes supported storage backend aliases.
func NormalizeStorageType(raw string) (string, error) {
	clean := strings.ToLower(strings.TrimSpace(raw))
	if clean == "" || clean == "json" {
		return "json", nil
	}
	switch clean {
	case "sqlite", "sqlite3":
		return "sqlite", nil
	case "mysql":
		return "mysql", nil
	case "mariadb":
		return "mariadb", nil
	case "postgres", "postgresql":
		return "postgresql", nil
	default:
		return "", fmt.Errorf("unsupported storage_type %q (expected json, sqlite, mysql, mariadb, or postgresql)", raw)
	}
}

// ValidateTablePrefix checks that table prefix only contains [a-zA-Z0-9_] up to 32 chars.
func ValidateTablePrefix(prefix string) error {
	if !tablePrefixRegex.MatchString(prefix) {
		return fmt.Errorf("invalid table_prefix %q: must match ^[a-zA-Z0-9_]{0,32}$", prefix)
	}
	return nil
}

// ResolvePassword resolves DB credentials in priority order: env var, inline value, password file.
func ResolvePassword(inline, filePath, envVar string) (string, error) {
	if envVar != "" {
		if v := os.Getenv(envVar); v != "" {
			return strings.TrimRight(v, "\r\n"), nil
		}
	}
	if strings.TrimSpace(inline) != "" {
		return inline, nil
	}
	fp := strings.TrimSpace(filePath)
	if fp != "" {
		b, err := os.ReadFile(fp)
		if err != nil {
			return "", fmt.Errorf("failed reading database password file %q: %w", fp, err)
		}
		return strings.TrimRight(string(b), "\r\n"), nil
	}
	return "", nil
}

// Open constructs a StorageProvider ("json", "sqlite", "mysql", "mariadb", or "postgresql").
func Open(storageType, storageDir string, sqlCfg SQLConfig) (StorageProvider, error) {
	norm, err := NormalizeStorageType(storageType)
	if err != nil {
		return nil, err
	}
	if norm == "json" {
		return NewJSONStorage(storageDir)
	}
	sqlCfg.Type = norm
	if sqlCfg.StorageDir == "" {
		sqlCfg.StorageDir = storageDir
	}
	return NewSQLStorage(sqlCfg)
}

// NewSQLStorage initializes a database connection pool, migrates schema v1, and seeds defaults if empty.
func NewSQLStorage(cfg SQLConfig) (*SQLStorage, error) {
	norm, err := NormalizeStorageType(cfg.Type)
	if err != nil {
		return nil, err
	}
	if norm == "json" {
		return nil, errors.New("NewSQLStorage requires a SQL storage type (sqlite, mysql, mariadb, postgresql)")
	}
	if cfg.TablePrefix == "" {
		cfg.TablePrefix = "stonyperms_"
	}
	if err := ValidateTablePrefix(cfg.TablePrefix); err != nil {
		return nil, err
	}

	dialect := Dialect(norm)
	password, err := ResolvePassword(cfg.Password, cfg.PasswordFile, "STONYPERMS_DB_PASSWORD")
	if err != nil {
		return nil, err
	}

	driverName, dsn, target, err := buildDriverAndDSN(dialect, cfg, password)
	if err != nil {
		return nil, err
	}

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s storage (%s): %w", dialect, target, err)
	}

	applyPoolSettings(db, dialect, cfg)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s storage (%s): %w", dialect, target, err)
	}

	s := &SQLStorage{
		db:         db,
		dialect:    dialect,
		prefix:     cfg.TablePrefix,
		storageDir: strings.TrimSpace(cfg.StorageDir),
		target:     target,
	}

	if dialect == DialectSQLite {
		pragmas := []string{
			"PRAGMA journal_mode=WAL",
			"PRAGMA synchronous=NORMAL",
			"PRAGMA foreign_keys=ON",
			"PRAGMA busy_timeout=5000",
		}
		for _, p := range pragmas {
			if _, pErr := db.Exec(p); pErr != nil {
				_ = db.Close()
				return nil, fmt.Errorf("sqlite pragma %q: %w", p, pErr)
			}
		}
	}

	if err := s.initSchemaAndSeed(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func buildDriverAndDSN(dialect Dialect, cfg SQLConfig, password string) (driverName, dsn, safeTarget string, err error) {
	timeoutSec := cfg.ConnectTimeoutSeconds
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	switch dialect {
	case DialectSQLite:
		file := strings.TrimSpace(cfg.File)
		if file == "" {
			base := strings.TrimSpace(cfg.StorageDir)
			if base == "" {
				base = filepath.Join("config", "stonyperms")
			}
			file = filepath.Join(base, "stonyperms.db")
		}
		if file != ":memory:" && !strings.HasPrefix(file, "file:") {
			if mkErr := os.MkdirAll(filepath.Dir(file), 0o755); mkErr != nil {
				return "", "", "", fmt.Errorf("create sqlite parent dir: %w", mkErr)
			}
		}
		return "sqlite", file, "sqlite:" + file, nil

	case DialectMySQL, DialectMariaDB:
		host := strings.TrimSpace(cfg.Host)
		if host == "" {
			host = "127.0.0.1"
		}
		port := cfg.Port
		if port <= 0 {
			port = 3306
		}
		dbName := strings.TrimSpace(cfg.Database)
		if dbName == "" {
			dbName = "stonyperms"
		}
		user := strings.TrimSpace(cfg.Username)
		if user == "" {
			user = "stonyperms"
		}
		mc := mysql.NewConfig()
		mc.User = user
		mc.Passwd = password
		mc.Net = "tcp"
		mc.Addr = fmt.Sprintf("%s:%d", host, port)
		mc.DBName = dbName
		mc.ParseTime = true
		mc.Timeout = time.Duration(timeoutSec) * time.Second
		mc.Params = map[string]string{
			"charset": "utf8mb4",
		}
		tlsVal := mapMySQLSSLMode(cfg.SSLMode)
		if tlsVal != "" {
			mc.TLSConfig = tlsVal
		}
		target := fmt.Sprintf("%s://%s@%s:%d/%s?prefix=%s", dialect, user, host, port, dbName, cfg.TablePrefix)
		return "mysql", mc.FormatDSN(), target, nil

	case DialectPostgreSQL:
		host := strings.TrimSpace(cfg.Host)
		if host == "" {
			host = "127.0.0.1"
		}
		port := cfg.Port
		if port <= 0 {
			port = 5432
		}
		dbName := strings.TrimSpace(cfg.Database)
		if dbName == "" {
			dbName = "stonyperms"
		}
		user := strings.TrimSpace(cfg.Username)
		if user == "" {
			user = "stonyperms"
		}
		sslMode := mapPostgresSSLMode(cfg.SSLMode)
		u := &url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(user, password),
			Host:   fmt.Sprintf("%s:%d", host, port),
			Path:   "/" + dbName,
		}
		q := url.Values{}
		q.Set("sslmode", sslMode)
		q.Set("connect_timeout", strconv.Itoa(timeoutSec))
		u.RawQuery = q.Encode()
		target := fmt.Sprintf("postgresql://%s@%s:%d/%s?sslmode=%s&prefix=%s", user, host, port, dbName, sslMode, cfg.TablePrefix)
		return "pgx", u.String(), target, nil

	default:
		return "", "", "", fmt.Errorf("unsupported dialect %q", dialect)
	}
}

func mapMySQLSSLMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "prefer", "preferred":
		return "preferred"
	case "disable", "false", "none":
		return ""
	case "require", "true", "verify-ca", "verify-full":
		return "true"
	case "skip-verify":
		return "skip-verify"
	default:
		return "preferred"
	}
}

func mapPostgresSSLMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "prefer", "preferred":
		return "prefer"
	case "disable", "false", "none":
		return "disable"
	case "allow":
		return "allow"
	case "require", "true", "skip-verify":
		return "require"
	case "verify-ca":
		return "verify-ca"
	case "verify-full":
		return "verify-full"
	default:
		return "prefer"
	}
}

func applyPoolSettings(db *sql.DB, dialect Dialect, cfg SQLConfig) {
	if dialect == DialectSQLite {
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		db.SetConnMaxLifetime(0)
		return
	}
	maxOpen := cfg.MaxOpenConns
	if maxOpen <= 0 {
		maxOpen = 10
	}
	maxIdle := cfg.MaxIdleConns
	if maxIdle <= 0 {
		maxIdle = 5
	}
	if maxIdle > maxOpen {
		maxIdle = maxOpen
	}
	lifetime := cfg.ConnMaxLifetimeSeconds
	if lifetime <= 0 {
		lifetime = 1800
	}
	db.SetMaxOpenConns(maxOpen)
	db.SetMaxIdleConns(maxIdle)
	db.SetConnMaxLifetime(time.Duration(lifetime) * time.Second)
}

// DescribeTarget returns a redacted connection descriptor safe for logs and status output.
func (s *SQLStorage) DescribeTarget() string {
	return s.target
}

// BaseDir returns the configured storage directory used for exports.
func (s *SQLStorage) BaseDir() string {
	return s.storageDir
}

// TablePrefix returns the validated SQL table prefix.
func (s *SQLStorage) TablePrefix() string {
	return s.prefix
}

// Dialect returns the configured database dialect.
func (s *SQLStorage) Dialect() Dialect {
	return s.dialect
}

// Close releases the underlying SQL connection pool.
func (s *SQLStorage) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *SQLStorage) tbl(name string) string {
	return s.prefix + name
}

func (s *SQLStorage) rebind(query string) string {
	if s.dialect != DialectPostgreSQL {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 16)
	arg := 1
	inQuote := false
	for i := 0; i < len(query); i++ {
		c := query[i]
		if c == '\'' {
			inQuote = !inQuote
			b.WriteByte(c)
			continue
		}
		if !inQuote && c == '?' {
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(arg))
			arg++
		} else {
			b.WriteByte(c)
		}
	}
	return b.String()
}

func (s *SQLStorage) ddlStatements() []string {
	p := s.prefix
	boolType := "BOOLEAN"
	autoPK := "INTEGER PRIMARY KEY AUTOINCREMENT"
	metaCollate := ""
	tableSuffix := ""

	switch s.dialect {
	case DialectPostgreSQL:
		autoPK = "BIGSERIAL PRIMARY KEY"
	case DialectMySQL, DialectMariaDB:
		autoPK = "BIGINT AUTO_INCREMENT PRIMARY KEY"
		metaCollate = " COLLATE utf8mb4_bin"
		tableSuffix = " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci"
	}

	isMySQL := s.dialect == DialectMySQL || s.dialect == DialectMariaDB
	groupNodesIdx := ""
	userNodesIdx := ""
	usersNameIdx := ""
	logsIdx := ""
	if isMySQL {
		groupNodesIdx = ", INDEX idx_perm_key (perm_key)"
		userNodesIdx = ", INDEX idx_perm_key (perm_key)"
		usersNameIdx = ", INDEX idx_username (username)"
		logsIdx = ", INDEX idx_ts_id (timestamp, id)"
	}

	stmts := []string{
		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %sschema_meta (
			version INTEGER PRIMARY KEY,
			applied_at BIGINT NOT NULL
		)%s`, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %sgroups (
			name VARCHAR(64) PRIMARY KEY,
			display_name VARCHAR(255) NOT NULL DEFAULT '',
			weight INTEGER NOT NULL DEFAULT 0
		)%s`, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %sgroup_nodes (
			group_name VARCHAR(64) NOT NULL,
			position INTEGER NOT NULL,
			perm_key VARCHAR(255) NOT NULL,
			value %s NOT NULL,
			expiry BIGINT NOT NULL DEFAULT 0,
			contexts TEXT NOT NULL,
			PRIMARY KEY (group_name, position),
			FOREIGN KEY (group_name) REFERENCES %sgroups(name) ON DELETE CASCADE%s
		)%s`, p, boolType, p, groupNodesIdx, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %sgroup_parents (
			group_name VARCHAR(64) NOT NULL,
			position INTEGER NOT NULL,
			parent_group VARCHAR(64) NOT NULL,
			expiry BIGINT NOT NULL DEFAULT 0,
			contexts TEXT NOT NULL,
			PRIMARY KEY (group_name, position),
			FOREIGN KEY (group_name) REFERENCES %sgroups(name) ON DELETE CASCADE
		)%s`, p, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %sgroup_affixes (
			group_name VARCHAR(64) NOT NULL,
			kind VARCHAR(8) NOT NULL,
			position INTEGER NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			value TEXT NOT NULL,
			expiry BIGINT NOT NULL DEFAULT 0,
			contexts TEXT NOT NULL,
			PRIMARY KEY (group_name, kind, position),
			FOREIGN KEY (group_name) REFERENCES %sgroups(name) ON DELETE CASCADE
		)%s`, p, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %sgroup_meta (
			group_name VARCHAR(64) NOT NULL,
			meta_key VARCHAR(128)%s NOT NULL,
			meta_value TEXT NOT NULL,
			PRIMARY KEY (group_name, meta_key),
			FOREIGN KEY (group_name) REFERENCES %sgroups(name) ON DELETE CASCADE
		)%s`, p, metaCollate, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %sgroup_meta_entries (
			group_name VARCHAR(64) NOT NULL,
			position INTEGER NOT NULL,
			meta_key VARCHAR(128) NOT NULL,
			meta_value TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			expiry BIGINT NOT NULL DEFAULT 0,
			contexts TEXT NOT NULL,
			PRIMARY KEY (group_name, position),
			FOREIGN KEY (group_name) REFERENCES %sgroups(name) ON DELETE CASCADE
		)%s`, p, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %susers (
			uuid CHAR(36) PRIMARY KEY,
			username VARCHAR(64) NOT NULL DEFAULT '',
			primary_group VARCHAR(64) NOT NULL DEFAULT 'default'%s
		)%s`, p, usersNameIdx, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %suser_nodes (
			user_uuid CHAR(36) NOT NULL,
			position INTEGER NOT NULL,
			perm_key VARCHAR(255) NOT NULL,
			value %s NOT NULL,
			expiry BIGINT NOT NULL DEFAULT 0,
			contexts TEXT NOT NULL,
			PRIMARY KEY (user_uuid, position),
			FOREIGN KEY (user_uuid) REFERENCES %susers(uuid) ON DELETE CASCADE%s
		)%s`, p, boolType, p, userNodesIdx, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %suser_parents (
			user_uuid CHAR(36) NOT NULL,
			position INTEGER NOT NULL,
			parent_group VARCHAR(64) NOT NULL,
			expiry BIGINT NOT NULL DEFAULT 0,
			contexts TEXT NOT NULL,
			PRIMARY KEY (user_uuid, position),
			FOREIGN KEY (user_uuid) REFERENCES %susers(uuid) ON DELETE CASCADE
		)%s`, p, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %suser_affixes (
			user_uuid CHAR(36) NOT NULL,
			kind VARCHAR(8) NOT NULL,
			position INTEGER NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			value TEXT NOT NULL,
			expiry BIGINT NOT NULL DEFAULT 0,
			contexts TEXT NOT NULL,
			PRIMARY KEY (user_uuid, kind, position),
			FOREIGN KEY (user_uuid) REFERENCES %susers(uuid) ON DELETE CASCADE
		)%s`, p, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %suser_meta (
			user_uuid CHAR(36) NOT NULL,
			meta_key VARCHAR(128)%s NOT NULL,
			meta_value TEXT NOT NULL,
			PRIMARY KEY (user_uuid, meta_key),
			FOREIGN KEY (user_uuid) REFERENCES %susers(uuid) ON DELETE CASCADE
		)%s`, p, metaCollate, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %suser_meta_entries (
			user_uuid CHAR(36) NOT NULL,
			position INTEGER NOT NULL,
			meta_key VARCHAR(128) NOT NULL,
			meta_value TEXT NOT NULL,
			priority INTEGER NOT NULL DEFAULT 0,
			expiry BIGINT NOT NULL DEFAULT 0,
			contexts TEXT NOT NULL,
			PRIMARY KEY (user_uuid, position),
			FOREIGN KEY (user_uuid) REFERENCES %susers(uuid) ON DELETE CASCADE
		)%s`, p, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %stracks (
			name VARCHAR(64) PRIMARY KEY
		)%s`, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %strack_groups (
			track_name VARCHAR(64) NOT NULL,
			position INTEGER NOT NULL,
			group_name VARCHAR(64) NOT NULL,
			PRIMARY KEY (track_name, position),
			FOREIGN KEY (track_name) REFERENCES %stracks(name) ON DELETE CASCADE
		)%s`, p, p, tableSuffix),

		fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %slogs (
			id %s,
			timestamp BIGINT NOT NULL,
			actor VARCHAR(128) NOT NULL,
			target_type VARCHAR(32) NOT NULL,
			target VARCHAR(128) NOT NULL,
			action TEXT NOT NULL%s
		)%s`, p, autoPK, logsIdx, tableSuffix),
	}

	if !isMySQL {
		stmts = append(stmts,
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%sgroup_nodes_key ON %sgroup_nodes (perm_key)`, p, p),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%suser_nodes_key ON %suser_nodes (perm_key)`, p, p),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%susers_username ON %susers (username)`, p, p),
			fmt.Sprintf(`CREATE INDEX IF NOT EXISTS idx_%slogs_ts_id ON %slogs (timestamp, id)`, p, p),
		)
	}
	return stmts
}

func (s *SQLStorage) initSchemaAndSeed() error {
	s.lockSQLiteWrite()
	defer s.unlockSQLiteWrite()

	for _, ddl := range s.ddlStatements() {
		if _, err := s.db.Exec(ddl); err != nil {
			return fmt.Errorf("apply schema DDL: %w", err)
		}
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().Unix()
	switch s.dialect {
	case DialectMySQL, DialectMariaDB:
		if _, err := tx.Exec(fmt.Sprintf(`INSERT IGNORE INTO %s (version, applied_at) VALUES (?, ?)`, s.tbl("schema_meta")), SchemaVersion, now); err != nil {
			return err
		}
	default:
		if _, err := tx.Exec(s.rebind(fmt.Sprintf(`INSERT INTO %s (version, applied_at) VALUES (?, ?) ON CONFLICT(version) DO NOTHING`, s.tbl("schema_meta"))), SchemaVersion, now); err != nil {
			return err
		}
	}

	var groupCount int
	if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, s.tbl("groups"))).Scan(&groupCount); err != nil {
		return err
	}
	if groupCount == 0 {
		def := model.NewGroup("default", 0)
		def.DisplayName = "&7Member"
		def.AddNode(model.NewNode("gate.command.server", true))
		def.AddNode(model.NewNode("gate.command.glist", true))
		def.AddNode(model.NewNode("gate.command.ping", true))
		def.AddPrefix(model.PriorityMeta{Priority: 10, Value: "&7[Member] "})
		if err := s.saveGroupTx(tx, def); err != nil {
			return err
		}

		admin := model.NewGroup("admin", 100)
		admin.DisplayName = "&cAdmin"
		admin.AddParent(model.NewInheritance("default"))
		admin.AddNode(model.NewNode("*", true))
		admin.AddNode(model.NewNode("stonyperms.*", true))
		admin.AddPrefix(model.PriorityMeta{Priority: 100, Value: "&c[Admin] "})
		if err := s.saveGroupTx(tx, admin); err != nil {
			return err
		}

		var trackCount int
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, s.tbl("tracks"))).Scan(&trackCount); err != nil {
			return err
		}
		if trackCount == 0 {
			ranks := model.NewTrack("ranks")
			ranks.Append("default")
			ranks.Append("admin")
			if err := s.saveTrackTx(tx, ranks); err != nil {
				return err
			}
		}

		var logCount int
		if err := tx.QueryRow(fmt.Sprintf(`SELECT COUNT(*) FROM %s`, s.tbl("logs"))).Scan(&logCount); err != nil {
			return err
		}
		if logCount == 0 {
			if err := s.appendLogTx(tx, model.NewLogEntry("System", "general", "system", "Initialized StonyPerms storage")); err != nil {
				return err
			}
		}
	}

	return tx.Commit()
}

func (s *SQLStorage) lockSQLiteWrite() {
	if s.dialect == DialectSQLite {
		s.writeMu.Lock()
	}
}

func (s *SQLStorage) unlockSQLiteWrite() {
	if s.dialect == DialectSQLite {
		s.writeMu.Unlock()
	}
}

// LoadAll loads all groups, tracks, users, and up to maxLogEntries most recent audit logs.
func (s *SQLStorage) LoadAll() (*DataBundle, error) {
	bundle := &DataBundle{
		Users:  make(map[uuid.UUID]*model.User),
		Groups: make(map[string]*model.Group),
		Tracks: make(map[string]*model.Track),
		Logs:   make([]model.LogEntry, 0),
	}

	if err := s.loadGroups(bundle); err != nil {
		return nil, err
	}
	if err := s.loadTracks(bundle); err != nil {
		return nil, err
	}
	if err := s.loadUsers(bundle); err != nil {
		return nil, err
	}
	logs, err := s.GetLogs(maxLogEntries)
	if err != nil {
		return nil, err
	}
	bundle.Logs = logs
	return bundle, nil
}

func (s *SQLStorage) loadGroups(bundle *DataBundle) error {
	rows, err := s.db.Query(fmt.Sprintf(`SELECT name, display_name, weight FROM %s ORDER BY name ASC`, s.tbl("groups")))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var name, displayName string
		var weight int
		if err := rows.Scan(&name, &displayName, &weight); err != nil {
			return err
		}
		norm := strings.ToLower(strings.TrimSpace(name))
		g := model.NewGroup(norm, weight)
		g.DisplayName = displayName
		bundle.Groups[norm] = g
	}
	if err := rows.Err(); err != nil {
		return err
	}

	nRows, err := s.db.Query(fmt.Sprintf(`SELECT group_name, perm_key, value, expiry, contexts FROM %s ORDER BY group_name ASC, position ASC`, s.tbl("group_nodes")))
	if err != nil {
		return err
	}
	defer func() { _ = nRows.Close() }()
	for nRows.Next() {
		var gName, key, ctxJSON string
		var val bool
		var expiry int64
		if err := nRows.Scan(&gName, &key, &val, &expiry, &ctxJSON); err != nil {
			return err
		}
		if g, ok := bundle.Groups[strings.ToLower(gName)]; ok {
			g.Nodes = append(g.Nodes, model.Node{
				Key:      key,
				Value:    val,
				Expiry:   expiry,
				Contexts: unmarshalContexts(ctxJSON),
			})
		}
	}
	if err := nRows.Err(); err != nil {
		return err
	}

	pRows, err := s.db.Query(fmt.Sprintf(`SELECT group_name, parent_group, expiry, contexts FROM %s ORDER BY group_name ASC, position ASC`, s.tbl("group_parents")))
	if err != nil {
		return err
	}
	defer func() { _ = pRows.Close() }()
	for pRows.Next() {
		var gName, parent, ctxJSON string
		var expiry int64
		if err := pRows.Scan(&gName, &parent, &expiry, &ctxJSON); err != nil {
			return err
		}
		if g, ok := bundle.Groups[strings.ToLower(gName)]; ok {
			g.Parents = append(g.Parents, model.Inheritance{
				Group:    strings.ToLower(strings.TrimSpace(parent)),
				Expiry:   expiry,
				Contexts: unmarshalContexts(ctxJSON),
			})
		}
	}
	if err := pRows.Err(); err != nil {
		return err
	}

	aRows, err := s.db.Query(fmt.Sprintf(`SELECT group_name, kind, priority, value, expiry, contexts FROM %s ORDER BY group_name ASC, kind ASC, position ASC`, s.tbl("group_affixes")))
	if err != nil {
		return err
	}
	defer func() { _ = aRows.Close() }()
	for aRows.Next() {
		var gName, kind, val, ctxJSON string
		var priority int
		var expiry int64
		if err := aRows.Scan(&gName, &kind, &priority, &val, &expiry, &ctxJSON); err != nil {
			return err
		}
		if g, ok := bundle.Groups[strings.ToLower(gName)]; ok {
			pm := model.PriorityMeta{
				Priority: priority,
				Value:    val,
				Expiry:   expiry,
				Contexts: unmarshalContexts(ctxJSON),
			}
			if kind == "suffix" {
				g.Suffixes = append(g.Suffixes, pm)
			} else {
				g.Prefixes = append(g.Prefixes, pm)
			}
		}
	}
	if err := aRows.Err(); err != nil {
		return err
	}

	mRows, err := s.db.Query(fmt.Sprintf(`SELECT group_name, meta_key, meta_value FROM %s`, s.tbl("group_meta")))
	if err != nil {
		return err
	}
	defer func() { _ = mRows.Close() }()
	for mRows.Next() {
		var gName, k, v string
		if err := mRows.Scan(&gName, &k, &v); err != nil {
			return err
		}
		if g, ok := bundle.Groups[strings.ToLower(gName)]; ok {
			if g.Meta == nil {
				g.Meta = make(map[string]string)
			}
			g.Meta[k] = v
		}
	}
	if err := mRows.Err(); err != nil {
		return err
	}

	meRows, err := s.db.Query(fmt.Sprintf(`SELECT group_name, meta_key, meta_value, priority, expiry, contexts FROM %s ORDER BY group_name ASC, position ASC`, s.tbl("group_meta_entries")))
	if err != nil {
		return err
	}
	defer func() { _ = meRows.Close() }()
	for meRows.Next() {
		var gName, k, v, ctxJSON string
		var priority int
		var expiry int64
		if err := meRows.Scan(&gName, &k, &v, &priority, &expiry, &ctxJSON); err != nil {
			return err
		}
		if g, ok := bundle.Groups[strings.ToLower(gName)]; ok {
			g.MetaEntries = append(g.MetaEntries, model.MetaEntry{
				Key:      k,
				Value:    v,
				Priority: priority,
				Expiry:   expiry,
				Contexts: unmarshalContexts(ctxJSON),
			})
		}
	}
	return meRows.Err()
}

func (s *SQLStorage) loadTracks(bundle *DataBundle) error {
	rows, err := s.db.Query(fmt.Sprintf(`SELECT name FROM %s ORDER BY name ASC`, s.tbl("tracks")))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		norm := strings.ToLower(strings.TrimSpace(name))
		bundle.Tracks[norm] = model.NewTrack(norm)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	gRows, err := s.db.Query(fmt.Sprintf(`SELECT track_name, group_name FROM %s ORDER BY track_name ASC, position ASC`, s.tbl("track_groups")))
	if err != nil {
		return err
	}
	defer func() { _ = gRows.Close() }()
	for gRows.Next() {
		var tName, gName string
		if err := gRows.Scan(&tName, &gName); err != nil {
			return err
		}
		if tr, ok := bundle.Tracks[strings.ToLower(tName)]; ok {
			tr.Groups = append(tr.Groups, strings.ToLower(strings.TrimSpace(gName)))
		}
	}
	return gRows.Err()
}

func (s *SQLStorage) loadUsers(bundle *DataBundle) error {
	rows, err := s.db.Query(fmt.Sprintf(`SELECT uuid, username, primary_group FROM %s`, s.tbl("users")))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var rawID, username, primary string
		if err := rows.Scan(&rawID, &username, &primary); err != nil {
			return err
		}
		uid, parseErr := uuid.Parse(strings.TrimSpace(rawID))
		if parseErr != nil {
			continue
		}
		u := model.NewUser(uid, username)
		if strings.TrimSpace(primary) != "" {
			u.PrimaryGroup = strings.ToLower(strings.TrimSpace(primary))
		}
		bundle.Users[uid] = u
	}
	if err := rows.Err(); err != nil {
		return err
	}

	nRows, err := s.db.Query(fmt.Sprintf(`SELECT user_uuid, perm_key, value, expiry, contexts FROM %s ORDER BY user_uuid ASC, position ASC`, s.tbl("user_nodes")))
	if err != nil {
		return err
	}
	defer func() { _ = nRows.Close() }()
	for nRows.Next() {
		var rawID, key, ctxJSON string
		var val bool
		var expiry int64
		if err := nRows.Scan(&rawID, &key, &val, &expiry, &ctxJSON); err != nil {
			return err
		}
		if uid, err := uuid.Parse(strings.TrimSpace(rawID)); err == nil {
			if u, ok := bundle.Users[uid]; ok {
				u.Nodes = append(u.Nodes, model.Node{
					Key:      key,
					Value:    val,
					Expiry:   expiry,
					Contexts: unmarshalContexts(ctxJSON),
				})
			}
		}
	}
	if err := nRows.Err(); err != nil {
		return err
	}

	pRows, err := s.db.Query(fmt.Sprintf(`SELECT user_uuid, parent_group, expiry, contexts FROM %s ORDER BY user_uuid ASC, position ASC`, s.tbl("user_parents")))
	if err != nil {
		return err
	}
	defer func() { _ = pRows.Close() }()
	for pRows.Next() {
		var rawID, parent, ctxJSON string
		var expiry int64
		if err := pRows.Scan(&rawID, &parent, &expiry, &ctxJSON); err != nil {
			return err
		}
		if uid, err := uuid.Parse(strings.TrimSpace(rawID)); err == nil {
			if u, ok := bundle.Users[uid]; ok {
				u.Parents = append(u.Parents, model.Inheritance{
					Group:    strings.ToLower(strings.TrimSpace(parent)),
					Expiry:   expiry,
					Contexts: unmarshalContexts(ctxJSON),
				})
			}
		}
	}
	if err := pRows.Err(); err != nil {
		return err
	}

	aRows, err := s.db.Query(fmt.Sprintf(`SELECT user_uuid, kind, priority, value, expiry, contexts FROM %s ORDER BY user_uuid ASC, kind ASC, position ASC`, s.tbl("user_affixes")))
	if err != nil {
		return err
	}
	defer func() { _ = aRows.Close() }()
	for aRows.Next() {
		var rawID, kind, val, ctxJSON string
		var priority int
		var expiry int64
		if err := aRows.Scan(&rawID, &kind, &priority, &val, &expiry, &ctxJSON); err != nil {
			return err
		}
		if uid, err := uuid.Parse(strings.TrimSpace(rawID)); err == nil {
			if u, ok := bundle.Users[uid]; ok {
				pm := model.PriorityMeta{
					Priority: priority,
					Value:    val,
					Expiry:   expiry,
					Contexts: unmarshalContexts(ctxJSON),
				}
				if kind == "suffix" {
					u.Suffixes = append(u.Suffixes, pm)
				} else {
					u.Prefixes = append(u.Prefixes, pm)
				}
			}
		}
	}
	if err := aRows.Err(); err != nil {
		return err
	}

	mRows, err := s.db.Query(fmt.Sprintf(`SELECT user_uuid, meta_key, meta_value FROM %s`, s.tbl("user_meta")))
	if err != nil {
		return err
	}
	defer func() { _ = mRows.Close() }()
	for mRows.Next() {
		var rawID, k, v string
		if err := mRows.Scan(&rawID, &k, &v); err != nil {
			return err
		}
		if uid, err := uuid.Parse(strings.TrimSpace(rawID)); err == nil {
			if u, ok := bundle.Users[uid]; ok {
				if u.Meta == nil {
					u.Meta = make(map[string]string)
				}
				u.Meta[k] = v
			}
		}
	}
	if err := mRows.Err(); err != nil {
		return err
	}

	meRows, err := s.db.Query(fmt.Sprintf(`SELECT user_uuid, meta_key, meta_value, priority, expiry, contexts FROM %s ORDER BY user_uuid ASC, position ASC`, s.tbl("user_meta_entries")))
	if err != nil {
		return err
	}
	defer func() { _ = meRows.Close() }()
	for meRows.Next() {
		var rawID, k, v, ctxJSON string
		var priority int
		var expiry int64
		if err := meRows.Scan(&rawID, &k, &v, &priority, &expiry, &ctxJSON); err != nil {
			return err
		}
		if uid, err := uuid.Parse(strings.TrimSpace(rawID)); err == nil {
			if u, ok := bundle.Users[uid]; ok {
				u.MetaEntries = append(u.MetaEntries, model.MetaEntry{
					Key:      k,
					Value:    v,
					Priority: priority,
					Expiry:   expiry,
					Contexts: unmarshalContexts(ctxJSON),
				})
			}
		}
	}
	return meRows.Err()
}

// SaveUser persists a user and all child tables transactionally.
func (s *SQLStorage) SaveUser(user *model.User) error {
	if user == nil || user.UUID == uuid.Nil {
		return fmt.Errorf("invalid user for save")
	}
	s.lockSQLiteWrite()
	defer s.unlockSQLiteWrite()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.saveUserTx(tx, user); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStorage) saveUserTx(tx *sql.Tx, user *model.User) error {
	uidStr := user.UUID.String()
	primary := strings.ToLower(strings.TrimSpace(user.PrimaryGroup))
	if primary == "" {
		primary = "default"
	}

	if s.dialect == DialectMySQL || s.dialect == DialectMariaDB {
		q := fmt.Sprintf(`INSERT INTO %s (uuid, username, primary_group) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE username = ?, primary_group = ?`, s.tbl("users"))
		if _, err := tx.Exec(q, uidStr, user.Username, primary, user.Username, primary); err != nil {
			return err
		}
	} else {
		q := s.rebind(fmt.Sprintf(`INSERT INTO %s (uuid, username, primary_group) VALUES (?, ?, ?) ON CONFLICT(uuid) DO UPDATE SET username = excluded.username, primary_group = excluded.primary_group`, s.tbl("users")))
		if _, err := tx.Exec(q, uidStr, user.Username, primary); err != nil {
			return err
		}
	}

	if err := s.deleteUserChildrenTx(tx, uidStr); err != nil {
		return err
	}
	if err := s.insertNodesTx(tx, "user_nodes", "user_uuid", uidStr, user.Nodes); err != nil {
		return err
	}
	if err := s.insertParentsTx(tx, "user_parents", "user_uuid", uidStr, user.Parents); err != nil {
		return err
	}
	if err := s.insertAffixesTx(tx, "user_affixes", "user_uuid", uidStr, user.Prefixes, user.Suffixes); err != nil {
		return err
	}
	if err := s.insertMetaTx(tx, "user_meta", "user_uuid", uidStr, user.Meta); err != nil {
		return err
	}
	return s.insertMetaEntriesTx(tx, "user_meta_entries", "user_uuid", uidStr, user.MetaEntries)
}

// DeleteUser removes a user and child rows inside a transaction.
func (s *SQLStorage) DeleteUser(id uuid.UUID) error {
	s.lockSQLiteWrite()
	defer s.unlockSQLiteWrite()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	uidStr := id.String()
	if err := s.deleteUserChildrenTx(tx, uidStr); err != nil {
		return err
	}
	if _, err := tx.Exec(s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE uuid = ?`, s.tbl("users"))), uidStr); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStorage) deleteUserChildrenTx(tx *sql.Tx, uidStr string) error {
	for _, tbl := range []string{"user_nodes", "user_parents", "user_affixes", "user_meta", "user_meta_entries"} {
		if _, err := tx.Exec(s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE user_uuid = ?`, s.tbl(tbl))), uidStr); err != nil {
			return err
		}
	}
	return nil
}

// SaveGroup persists a group and all child tables transactionally.
func (s *SQLStorage) SaveGroup(group *model.Group) error {
	if group == nil || strings.TrimSpace(group.Name) == "" {
		return fmt.Errorf("invalid group for save")
	}
	s.lockSQLiteWrite()
	defer s.unlockSQLiteWrite()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.saveGroupTx(tx, group); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStorage) saveGroupTx(tx *sql.Tx, group *model.Group) error {
	name := strings.ToLower(strings.TrimSpace(group.Name))
	if s.dialect == DialectMySQL || s.dialect == DialectMariaDB {
		q := fmt.Sprintf(`INSERT INTO %s (name, display_name, weight) VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE display_name = ?, weight = ?`, s.tbl("groups"))
		if _, err := tx.Exec(q, name, group.DisplayName, group.Weight, group.DisplayName, group.Weight); err != nil {
			return err
		}
	} else {
		q := s.rebind(fmt.Sprintf(`INSERT INTO %s (name, display_name, weight) VALUES (?, ?, ?) ON CONFLICT(name) DO UPDATE SET display_name = excluded.display_name, weight = excluded.weight`, s.tbl("groups")))
		if _, err := tx.Exec(q, name, group.DisplayName, group.Weight); err != nil {
			return err
		}
	}

	if err := s.deleteGroupChildrenTx(tx, name); err != nil {
		return err
	}
	if err := s.insertNodesTx(tx, "group_nodes", "group_name", name, group.Nodes); err != nil {
		return err
	}
	if err := s.insertParentsTx(tx, "group_parents", "group_name", name, group.Parents); err != nil {
		return err
	}
	if err := s.insertAffixesTx(tx, "group_affixes", "group_name", name, group.Prefixes, group.Suffixes); err != nil {
		return err
	}
	if err := s.insertMetaTx(tx, "group_meta", "group_name", name, group.Meta); err != nil {
		return err
	}
	return s.insertMetaEntriesTx(tx, "group_meta_entries", "group_name", name, group.MetaEntries)
}

// DeleteGroup removes a group and its child rows transactionally.
func (s *SQLStorage) DeleteGroup(name string) error {
	key := strings.ToLower(strings.TrimSpace(name))
	s.lockSQLiteWrite()
	defer s.unlockSQLiteWrite()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.deleteGroupChildrenTx(tx, key); err != nil {
		return err
	}
	if _, err := tx.Exec(s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE name = ?`, s.tbl("groups"))), key); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStorage) deleteGroupChildrenTx(tx *sql.Tx, key string) error {
	for _, tbl := range []string{"group_nodes", "group_parents", "group_affixes", "group_meta", "group_meta_entries"} {
		if _, err := tx.Exec(s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE group_name = ?`, s.tbl(tbl))), key); err != nil {
			return err
		}
	}
	return nil
}

// SaveTrack persists a track and its ordered groups transactionally.
func (s *SQLStorage) SaveTrack(track *model.Track) error {
	if track == nil || strings.TrimSpace(track.Name) == "" {
		return fmt.Errorf("invalid track for save")
	}
	s.lockSQLiteWrite()
	defer s.unlockSQLiteWrite()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if err := s.saveTrackTx(tx, track); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStorage) saveTrackTx(tx *sql.Tx, track *model.Track) error {
	name := strings.ToLower(strings.TrimSpace(track.Name))
	if s.dialect == DialectMySQL || s.dialect == DialectMariaDB {
		q := fmt.Sprintf(`INSERT IGNORE INTO %s (name) VALUES (?)`, s.tbl("tracks"))
		if _, err := tx.Exec(q, name); err != nil {
			return err
		}
	} else {
		q := s.rebind(fmt.Sprintf(`INSERT INTO %s (name) VALUES (?) ON CONFLICT(name) DO NOTHING`, s.tbl("tracks")))
		if _, err := tx.Exec(q, name); err != nil {
			return err
		}
	}

	if _, err := tx.Exec(s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE track_name = ?`, s.tbl("track_groups"))), name); err != nil {
		return err
	}
	if len(track.Groups) > 0 {
		ins := s.rebind(fmt.Sprintf(`INSERT INTO %s (track_name, position, group_name) VALUES (?, ?, ?)`, s.tbl("track_groups")))
		stmt, err := tx.Prepare(ins)
		if err != nil {
			return err
		}
		defer func() { _ = stmt.Close() }()
		for i, g := range track.Groups {
			if _, err := stmt.Exec(name, i, strings.ToLower(strings.TrimSpace(g))); err != nil {
				return err
			}
		}
	}
	return nil
}

// DeleteTrack deletes a track and its ordered group list atomically.
func (s *SQLStorage) DeleteTrack(name string) error {
	key := strings.ToLower(strings.TrimSpace(name))
	s.lockSQLiteWrite()
	defer s.unlockSQLiteWrite()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE track_name = ?`, s.tbl("track_groups"))), key); err != nil {
		return err
	}
	if _, err := tx.Exec(s.rebind(fmt.Sprintf(`DELETE FROM %s WHERE name = ?`, s.tbl("tracks"))), key); err != nil {
		return err
	}
	return tx.Commit()
}

// AppendLog inserts a new audit log row.
func (s *SQLStorage) AppendLog(entry model.LogEntry) error {
	s.lockSQLiteWrite()
	defer s.unlockSQLiteWrite()

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := s.appendLogTx(tx, entry); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLStorage) appendLogTx(tx *sql.Tx, entry model.LogEntry) error {
	q := s.rebind(fmt.Sprintf(`INSERT INTO %s (timestamp, actor, target_type, target, action) VALUES (?, ?, ?, ?, ?)`, s.tbl("logs")))
	_, err := tx.Exec(q, entry.Timestamp, entry.Actor, entry.TargetType, entry.Target, entry.Action)
	return err
}

// GetLogs returns up to limit recent log entries ordered chronologically (oldest first), matching JSONStorage.
func (s *SQLStorage) GetLogs(limit int) ([]model.LogEntry, error) {
	if limit <= 0 {
		limit = maxLogEntries
	}
	q := s.rebind(fmt.Sprintf(`SELECT timestamp, actor, target_type, target, action FROM %s ORDER BY id DESC LIMIT ?`, s.tbl("logs")))
	rows, err := s.db.Query(q, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]model.LogEntry, 0)
	for rows.Next() {
		var e model.LogEntry
		if err := rows.Scan(&e.Timestamp, &e.Actor, &e.TargetType, &e.Target, &e.Action); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

func (s *SQLStorage) insertNodesTx(tx *sql.Tx, table, ownerCol, ownerVal string, nodes []model.Node) error {
	if len(nodes) == 0 {
		return nil
	}
	q := s.rebind(fmt.Sprintf(`INSERT INTO %s (%s, position, perm_key, value, expiry, contexts) VALUES (?, ?, ?, ?, ?, ?)`, s.tbl(table), ownerCol))
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, n := range nodes {
		if _, err := stmt.Exec(ownerVal, i, n.Key, n.Value, n.Expiry, marshalContexts(n.Contexts)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStorage) insertParentsTx(tx *sql.Tx, table, ownerCol, ownerVal string, parents []model.Inheritance) error {
	if len(parents) == 0 {
		return nil
	}
	q := s.rebind(fmt.Sprintf(`INSERT INTO %s (%s, position, parent_group, expiry, contexts) VALUES (?, ?, ?, ?, ?)`, s.tbl(table), ownerCol))
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, p := range parents {
		if _, err := stmt.Exec(ownerVal, i, strings.ToLower(strings.TrimSpace(p.Group)), p.Expiry, marshalContexts(p.Contexts)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStorage) insertAffixesTx(tx *sql.Tx, table, ownerCol, ownerVal string, prefixes, suffixes []model.PriorityMeta) error {
	if len(prefixes) == 0 && len(suffixes) == 0 {
		return nil
	}
	q := s.rebind(fmt.Sprintf(`INSERT INTO %s (%s, kind, position, priority, value, expiry, contexts) VALUES (?, ?, ?, ?, ?, ?, ?)`, s.tbl(table), ownerCol))
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, p := range prefixes {
		if _, err := stmt.Exec(ownerVal, "prefix", i, p.Priority, p.Value, p.Expiry, marshalContexts(p.Contexts)); err != nil {
			return err
		}
	}
	for i, sf := range suffixes {
		if _, err := stmt.Exec(ownerVal, "suffix", i, sf.Priority, sf.Value, sf.Expiry, marshalContexts(sf.Contexts)); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStorage) insertMetaTx(tx *sql.Tx, table, ownerCol, ownerVal string, meta map[string]string) error {
	if len(meta) == 0 {
		return nil
	}
	q := s.rebind(fmt.Sprintf(`INSERT INTO %s (%s, meta_key, meta_value) VALUES (?, ?, ?)`, s.tbl(table), ownerCol))
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := stmt.Exec(ownerVal, k, meta[k]); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLStorage) insertMetaEntriesTx(tx *sql.Tx, table, ownerCol, ownerVal string, entries []model.MetaEntry) error {
	if len(entries) == 0 {
		return nil
	}
	q := s.rebind(fmt.Sprintf(`INSERT INTO %s (%s, position, meta_key, meta_value, priority, expiry, contexts) VALUES (?, ?, ?, ?, ?, ?, ?)`, s.tbl(table), ownerCol))
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()
	for i, e := range entries {
		if _, err := stmt.Exec(ownerVal, i, e.Key, e.Value, e.Priority, e.Expiry, marshalContexts(e.Contexts)); err != nil {
			return err
		}
	}
	return nil
}

func marshalContexts(ctx map[string]string) string {
	if len(ctx) == 0 {
		return "{}"
	}
	norm := make(map[string]string, len(ctx))
	for k, v := range ctx {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk != "" {
			norm[lk] = strings.TrimSpace(v)
		}
	}
	if len(norm) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(norm)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func unmarshalContexts(raw string) map[string]string {
	clean := strings.TrimSpace(raw)
	if clean == "" || clean == "{}" || clean == "null" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(clean), &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}
