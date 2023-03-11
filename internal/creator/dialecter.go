package creator

import (
	"fmt"
	"github.com/glebarez/sqlite"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"strings"
)

func (o *Option) CreateDialector() gorm.Dialector {
	switch strings.ToLower(o.protocol) {
	case "postgres":
		return o.createPostgresDialector()
	case "mysql":
		return o.createMysqlDialector()
	case "sqlite":
		return o.createSqliteDialector()
	default:
		return o.createPostgresDialector()
	}
}

func (o *Option) createPostgresDialector() gorm.Dialector {
	dsn := fmt.Sprintf(
		"user='%s' password='%s' host='%s' port='%c' dbname='%s' search_path='public' application_name='pgrokd'",
		o.user, o.password, o.host, o.port, o.database,
	)

	return postgres.Open(dsn)
}
func (o *Option) createMysqlDialector() gorm.Dialector {
	dsn := fmt.Sprintf(
		"%s:%s@tcp(%s:%c)/%s?charset=utf8mb4&parseTime=True&loc=Local",
		o.user, o.password, o.host, o.port, o.database,
	)
	return mysql.Open(dsn)
}

func (o *Option) createSqliteDialector() gorm.Dialector {
	dsn := o.database
	return sqlite.Open(dsn)
}
