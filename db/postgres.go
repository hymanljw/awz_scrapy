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
func NewPostgresDB() (*PostgresDB, string, error) {
	// 获取当前工作目录
	wd, err := os.Getwd()
	if err != nil {
		return nil, "", fmt.Errorf("获取工作目录失败: %v", err)
	}

	// 加载.env文件
	envPath := filepath.Join(wd, ".env")
	err = godotenv.Load(envPath)
	if err != nil {
		return nil, "", fmt.Errorf("加载.env文件失败: %v", err)
	}

	// 构建连接字符串
	connStr := os.Getenv("PG_URL")

	// 创建连接池
	pool, err := pgxpool.Connect(context.Background(), connStr)
	if err != nil {
		return nil, "", fmt.Errorf("连接数据库失败: %v", err)
	}

	convHost := os.Getenv("CONV_HOST")
	return &PostgresDB{pool: pool}, convHost, nil
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

// GetMongoConfig 查询configs表中type="mongo"的记录，返回values
func (db *PostgresDB) GetMongoConfig() (string, error) {
	var values string

	// 执行查询
	query := "SELECT values FROM configs WHERE type = $1"
	err := db.pool.QueryRow(context.Background(), query, "mongo").Scan(&values)
	if err != nil {
		return "", fmt.Errorf("查询mongo配置失败: %v", err)
	}

	return values, nil
}

// ExampleUsage 示例使用方法
func ExampleUsage() {
	// 创建数据库连接
	db, conv, err := NewPostgresDB()
	if err != nil {
		fmt.Printf("创建数据库连接失败: %v\n", err)
		return
	}
	defer db.Close()
	fmt.Println(conv)

	// 获取clash配置
	clashConfig, err := db.GetClashConfig()
	if err != nil {
		fmt.Printf("获取clash配置失败: %v\n", err)
		return
	}

	fmt.Printf("Clash配置: %s\n", clashConfig)
}
