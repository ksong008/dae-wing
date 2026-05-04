//go:build !(mips || mipsle || mips64le || mips64)

package sqlite

import (
	"strings"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func Open(dsn string) gorm.Dialector {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	return sqlite.Open(dsn + sep + "_pragma=foreign_keys(1)")
}
