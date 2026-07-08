// 本文件负责云端 PostgreSQL 自动迁移，并记录已执行迁移，避免重复覆盖系统配置。
package httpapi

import (
	"database/sql"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func RunMigrations(db *sql.DB) error {
	d := "db/migrations"
	es, err := os.ReadDir(d)
	if err != nil {
		return err
	}
	var fs []string
	for _, e := range es {
		n := e.Name()
		if !e.IsDir() && strings.HasSuffix(n, ".sql") && !strings.HasSuffix(n, ".down.sql") {
			fs = append(fs, n)
		}
	}
	sort.Strings(fs)
	if err := ensureMigrationTable(db); err != nil {
		return err
	}
	if ok, err := shouldBootstrapExistingDatabase(db); err != nil {
		return err
	} else if ok {
		log.Println("[migrate] existing database detected, mark current migrations as applied")
		return markMigrationsApplied(db, fs)
	}
	for _, f := range fs {
		applied, err := migrationApplied(db, f)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		b, _ := os.ReadFile(filepath.Join(d, f))
		log.Println("[migrate]", f)
		if _, err := db.Exec(string(b)); err != nil {
			return err
		}
		if err := markMigrationApplied(db, f); err != nil {
			return err
		}
	}
	log.Println("[migrate] done")
	return nil
}

// ensureMigrationTable 创建迁移记录表。
func ensureMigrationTable(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)
	`)
	return err
}

// shouldBootstrapExistingDatabase 判断是否是已运行过的旧数据库。
func shouldBootstrapExistingDatabase(db *sql.DB) (bool, error) {
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		return false, err
	}
	if count > 0 {
		return false, nil
	}
	var exists bool
	err := db.QueryRow(`
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name IN ('system_configs', 'users', 'tasks')
		)
	`).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// markMigrationsApplied 批量标记当前代码中的迁移已执行。
func markMigrationsApplied(db *sql.DB, filenames []string) error {
	for _, filename := range filenames {
		if err := markMigrationApplied(db, filename); err != nil {
			return err
		}
	}
	return nil
}

// migrationApplied 判断指定迁移是否已执行。
func migrationApplied(db *sql.DB, filename string) (bool, error) {
	var exists bool
	err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename=$1)`, filename).Scan(&exists)
	return exists, err
}

// markMigrationApplied 标记指定迁移已执行。
func markMigrationApplied(db *sql.DB, filename string) error {
	_, err := db.Exec(
		`INSERT INTO schema_migrations (filename) VALUES ($1) ON CONFLICT (filename) DO NOTHING`,
		filename,
	)
	return err
}
