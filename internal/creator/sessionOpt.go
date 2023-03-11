package creator

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/log"
	"github.com/flamego/session"
	"github.com/flamego/session/mysql"
	"github.com/flamego/session/postgres"
)

func (o *Option) CreateSessionOpt() session.Options {

	switch strings.ToLower(o.protocol) {
	case "postgres":
		return o.createPostgresqlSessionOpt()
	case "mysql":
		return o.createMysqlSessionOpt()
	case "sqlite":
		return o.createFileSessionOpt()
	default:
		return o.createPostgresqlSessionOpt()
	}
}

func (o *Option) createPostgresqlSessionOpt() session.Options {
	return session.Options{
		Initer: postgres.Initer(),
		Config: postgres.Config{
			DSN: fmt.Sprintf("postgres://%s:%s@%s:%d/%s",
				o.user,
				o.password,
				o.host,
				o.port,
				o.database,
			),
			Table:     "sessions",
			InitTable: true,
		},
		ErrorFunc: func(err error) {
			log.Error("session", "error", err)
		},
	}
}

func (o *Option) createMysqlSessionOpt() session.Options {
	return session.Options{
		Initer: mysql.Initer(),
		Config: mysql.Config{
			DSN: fmt.Sprintf("%s:%s@tcp(%s:%c)/%s?charset=utf8mb4&parseTime=True&loc=Local",
				o.user,
				o.password,
				o.host,
				o.port,
				o.database,
			),
			Table:     "sessions",
			InitTable: true,
		},
		ErrorFunc: func(err error) {
			log.Error("session", "error", err)
		},
	}
}

func (o *Option) createFileSessionOpt() session.Options {
	wd, _ := os.Getwd()
	return session.Options{
		Initer: session.FileIniter(),
		Config: session.FileConfig{
			RootDir: filepath.Join(wd, "sessions"),
		},
		ErrorFunc: func(err error) {
			log.Error("session", "error", err)
		},
	}
}
