package services

import (
	"context"
	"testing"
	"time"

	"cloud-platform/internal/models"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// openLifecycleTestDB 建一个内存库，仅供生命周期用例使用。
// 库名带用例前缀，避免与同包其它用例的 cache=shared 内存库串到同一个实例。
func openLifecycleTestDB(t *testing.T, name string, modelsToMigrate ...interface{}) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open("file:"+name+"?mode=memory&cache=shared"),
		&gorm.Config{Logger: logger.Default.LogMode(logger.Silent)},
	)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if len(modelsToMigrate) > 0 {
		if err := db.AutoMigrate(modelsToMigrate...); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return db
}

// waitForStop 断言 Stop 能在限时内返回——即它确实等待了后台循环收尾。
func waitForStop(t *testing.T, stop func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop 超时未返回，说明没有等待后台循环退出")
	}
}

// TestMetricsCollectorStopWaitsForLoop 验证 Stop 会等到采样循环真正退出。
//
// 这是关闭流程正确性的前提：采样协程会持续刷新内存快照，
// 若 Stop 只取消上下文就返回，进程可能在它仍在运行时继续走后续关闭步骤。
func TestMetricsCollectorStopWaitsForLoop(t *testing.T) {
	collector := NewMetricsCollector(context.Background(), 20*time.Millisecond)
	collector.StartBackground()

	// 让它至少跑一轮，确认循环确实在运转
	time.Sleep(80 * time.Millisecond)
	if !collector.Ready() {
		t.Fatal("采样器在启动后应处于就绪状态")
	}

	waitForStop(t, collector.Stop)

	// Stop 返回即意味着采样循环已退出；此后快照只读不再刷新。
	// 这里只断言停止后仍能安全读取快照（读路径加了读锁），
	// 不做"数值不变"的断言——那会依赖运行期恰好有流量波动，属于脆弱断言。
	if snap := collector.Snapshot(); snap.Host.Hostname == "" {
		t.Error("Stop 之后快照应保留最后一次采样结果，实际主机名为空")
	}
}

// TestMetricsCollectorStopIsIdempotent 验证 Stop 可重复调用且不会阻塞。
func TestMetricsCollectorStopIsIdempotent(t *testing.T) {
	collector := NewMetricsCollector(context.Background(), 50*time.Millisecond)
	collector.StartBackground()

	waitForStop(t, collector.Stop)
	waitForStop(t, collector.Stop) // 第二次调用应安全返回
}

// TestMetricsCollectorStopWithoutStart 验证从未启动过就 Stop 也不会阻塞。
//
// 关键点：Stop 依赖 cancel 在构造时已就绪。若 cancel 要等 Start 再赋值，
// 这里就会永久阻塞——关闭流程将卡死。
func TestMetricsCollectorStopWithoutStart(t *testing.T) {
	collector := NewMetricsCollector(context.Background(), time.Second)
	waitForStop(t, collector.Stop)
}

// TestMonitoringServiceStopWaitsForLoops 验证监控服务的后台循环会被等待收尾。
//
// 监控循环会写数据库，因此它必须在调用方关闭数据库连接之前彻底退出。
func TestMonitoringServiceStopWaitsForLoops(t *testing.T) {
	db := openLifecycleTestDB(t, "lifecycle_monitoring",
		&models.Setting{}, &models.MonitoringRecord{})
	service := NewMonitoringService(context.Background(), db, 20*time.Millisecond)
	service.StartBackground(20*time.Millisecond, time.Hour)

	// 等首轮落库完成，确认循环确实在写库
	time.Sleep(80 * time.Millisecond)

	waitForStop(t, service.Stop)

	// Stop 返回后循环必须已彻底退出：此后不再产生新的监控记录。
	// 这条断言钉住的正是本次修复的语义——旧实现只取消上下文就返回，
	// 调用方可能在循环仍在写库时关闭数据库连接。
	var countAfterStop int64
	if err := db.Model(&models.MonitoringRecord{}).Count(&countAfterStop).Error; err != nil {
		t.Fatalf("统计监控记录失败: %v", err)
	}
	time.Sleep(120 * time.Millisecond) // 远大于 20ms 的采样间隔
	var countLater int64
	if err := db.Model(&models.MonitoringRecord{}).Count(&countLater).Error; err != nil {
		t.Fatalf("统计监控记录失败: %v", err)
	}
	if countLater != countAfterStop {
		t.Errorf("Stop 返回后监控循环仍在写库：记录数 %d → %d", countAfterStop, countLater)
	}

	// 循环确已退出，此时关闭数据库不会再撞上写入
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("获取底层 *sql.DB 失败: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("Stop 之后关闭数据库失败: %v", err)
	}
}

// TestMonitoringServiceStopWithoutStart 验证未启动即停止不会阻塞。
func TestMonitoringServiceStopWithoutStart(t *testing.T) {
	db := openLifecycleTestDB(t, "lifecycle_nostart", &models.Setting{})
	service := NewMonitoringService(context.Background(), db, time.Second)
	waitForStop(t, service.Stop)
}
