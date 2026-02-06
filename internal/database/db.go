package database

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/log"
	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/pgrok/pgrok/internal/conf"
)

// DB is the database handle.
type DB struct {
	*gorm.DB
	embeddedServerHandle *embeddedpostgres.EmbeddedPostgres
}

func assemblePostgresConfig(dbSettings *conf.Database, workspace string) embeddedpostgres.Config {
	return embeddedpostgres.DefaultConfig().
		Username(dbSettings.User).
		Password(dbSettings.Password).
		Database(dbSettings.Database).
		Port(uint32(dbSettings.Port)).
		DataPath(filepath.Join(workspace, "pgrok_pgdata")).
		RuntimePath(filepath.Join(workspace, "pgrok_pgruntime")).
		BinariesPath("")
}

func bootEmbeddedServer(dbSettings *conf.Database) (*embeddedpostgres.EmbeddedPostgres, error) {
	workspace, mkErr := os.MkdirTemp("", "pgrokd-pg-")
	if mkErr != nil {
		return nil, errors.Wrap(mkErr, "mktemp workspace failed")
	}

	pgConfig := assemblePostgresConfig(dbSettings, workspace)
	serverHandle := embeddedpostgres.NewDatabase(pgConfig)
	bootErr := serverHandle.Start()
	if bootErr != nil {
		os.RemoveAll(workspace)
		return nil, errors.Wrap(bootErr, "embedded server boot failed")
	}

	return serverHandle, nil
}

func resolveHostAndPort(dbSettings *conf.Database) (string, int) {
	if dbSettings.EnableEmbedded {
		return "localhost", dbSettings.Port
	}
	return dbSettings.Host, dbSettings.Port
}

// New returns a new database handle with given configuration.
func New(logWriter io.Writer, config *conf.Database) (*DB, error) {
	if config == nil {
		return nil, errors.New("no database config provided")
	}

	dbHandle := &DB{}
	var bootFailure bool
	defer func() {
		if bootFailure && dbHandle.embeddedServerHandle != nil {
			dbHandle.embeddedServerHandle.Stop()
		}
	}()
	bootFailure = true

	if config.EnableEmbedded {
		serverHandle, bootErr := bootEmbeddedServer(config)
		if bootErr != nil {
			return nil, bootErr
		}
		dbHandle.embeddedServerHandle = serverHandle
	}

	pgHost, pgPort := resolveHostAndPort(config)

	level := logger.Info
	if flamego.Env() == flamego.EnvTypeProd {
		level = logger.Warn
	}

	// NOTE: AutoMigrate does not respect logger passed in gorm.Config.
	logger.Default = logger.New(
		&gormLogger{
			Logger: log.NewWithOptions(
				logWriter,
				log.Options{
					TimeFormat:      time.DateTime,
					Level:           log.DebugLevel,
					Prefix:          "gorm",
					ReportTimestamp: true,
				},
			),
		},
		logger.Config{
			SlowThreshold: 1000 * time.Millisecond,
			LogLevel:      level,
		},
	)

	dsn := fmt.Sprintf(
		"user='%s' password='%s' host='%s' port='%d' dbname='%s' search_path='public' application_name='pgrokd' client_encoding=UTF8",
		config.User, config.Password, pgHost, pgPort, config.Database,
	)
	db, err := gorm.Open(
		postgres.Open(dsn),
		&gorm.Config{
			SkipDefaultTransaction: true,
			NowFunc: func() time.Time {
				return time.Now().UTC().Truncate(time.Microsecond)
			},
		},
	)
	if err != nil {
		return nil, errors.Wrap(err, "open database")
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, errors.Wrap(err, "get underlying *sql.DB")
	}
	sqlDB.SetMaxOpenConns(30)
	sqlDB.SetMaxIdleConns(30)
	sqlDB.SetConnMaxLifetime(time.Minute)

	err = db.AutoMigrate(&Principal{}, &HostKey{})
	if err != nil {
		return nil, errors.Wrap(err, "auto migrate")
	}

	dbHandle.DB = db
	bootFailure = false
	return dbHandle, nil
}

// Terminate stops the embedded server if present.
func (db *DB) Terminate() error {
	if db.embeddedServerHandle != nil {
		return db.embeddedServerHandle.Stop()
	}
	return nil
}

// gormLogger is a wrapper of io.Writer for the GORM's logger.Writer.
type gormLogger struct {
	*log.Logger
}

func (l *gormLogger) Printf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	print := l.Debug
	if strings.Contains(msg, "[error]") {
		print = l.Error
	}
	print(msg)
}
