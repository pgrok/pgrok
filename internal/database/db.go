package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/flamego/flamego"
	"github.com/pkg/errors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
	"unknwon.dev/x/logx"

	"github.com/pgrok/pgrok/internal/conf"
)

// DB is the database handle.
type DB struct {
	*gorm.DB
}

// New returns a new database handle with given configuration.
func New(logger *logx.Logger, config *conf.Database) (*DB, error) {
	if config == nil {
		return nil, errors.New("no database config provided")
	}

	level := gormlogger.Info
	if flamego.Env() == flamego.EnvTypeProd {
		level = gormlogger.Warn
	}

	// NOTE: AutoMigrate does not respect logger passed in gorm.Config.
	gormlogger.Default = gormlogger.New(
		&gormLogger{Logger: logger.Scoped("gorm")},
		gormlogger.Config{
			SlowThreshold: 1000 * time.Millisecond,
			LogLevel:      level,
		},
	)

	dsn := fmt.Sprintf(
		"user='%s' password='%s' host='%s' port='%d' dbname='%s' search_path='public' application_name='pgrokd' client_encoding=UTF8",
		config.User, config.Password, config.Host, config.Port, config.Database,
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
	return &DB{db}, nil
}

// gormLogger adapts *logx.Logger to GORM's logger.Writer interface so that
// database log output flows through the application's structured logging
// pipeline.
type gormLogger struct {
	*logx.Logger
}

func (l *gormLogger) Printf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	print := l.Debug
	if strings.Contains(msg, "[error]") {
		print = l.Error
	}
	print(msg)
}
