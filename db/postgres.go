package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/jackc/pgx/v4/pgxpool"
	"github.com/joho/godotenv"
)

// PostgresDB 是PostgreSQL数据库连接的工具类
type PostgresDB struct {
	pool *pgxpool.Pool
}

// NewPostgresDB 创建一个新的PostgresDB实例
func NewPostgresDB() (*PostgresDB, error) {
	// 获取当前工作目录
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("获取工作目录失败: %v", err)
	}

	// 加载.env文件
	envPath := filepath.Join(wd, ".env")
	err = godotenv.Load(envPath)
	if err != nil {
		return nil, fmt.Errorf("加载.env文件失败: %v", err)
	}

	// 从环境变量获取数据库连接信息
	dbHost := os.Getenv("DB_HOST")
	dbPort := os.Getenv("DB_PORT")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")

	// 构建连接字符串
	connStr := fmt.Sprintf("postgres://%s:%s@%s:%s/%s",
		dbUser, dbPassword, dbHost, dbPort, dbName)

	// 创建连接池
	pool, err := pgxpool.Connect(context.Background(), connStr)
	if err != nil {
		return nil, fmt.Errorf("连接数据库失败: %v", err)
	}

	return &PostgresDB{pool: pool}, nil
}

// Close 关闭数据库连接
func (db *PostgresDB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// GetClashConfig 查询configs表中type="clash"的记录，返回values
func (db *PostgresDB) GetClashConfig() (string, error) {
	var values string

	// 执行查询
	query := "SELECT values FROM configs WHERE type = $1"
	err := db.pool.QueryRow(context.Background(), query, "clash").Scan(&values)
	if err != nil {
		return "", fmt.Errorf("查询clash配置失败: %v", err)
	}

	return values, nil
}

// ExampleUsage 示例使用方法
func ExampleUsage() {
	// 创建数据库连接
	db, err := NewPostgresDB()
	if err != nil {
		fmt.Printf("创建数据库连接失败: %v\n", err)
		return
	}
	defer db.Close()

	// 获取clash配置
	clashConfig, err := db.GetClashConfig()
	if err != nil {
		fmt.Printf("获取clash配置失败: %v\n", err)
		return
	}

	fmt.Printf("Clash配置: %s\n", clashConfig)
}
