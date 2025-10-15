// 系统监控相关类型定义

export interface CPUStats {
  usage_percent: number;
  cores: number;
  per_core: number[];
}

export interface MemoryStats {
  total: number;
  used: number;
  available: number;
  used_percent: number;
}

export interface DiskStats {
  total: number;
  used: number;
  free: number;
  used_percent: number;
}

export interface NetworkStats {
  bytes_sent: number;
  bytes_recv: number;
  packets_sent: number;
  packets_recv: number;
  speed_sent: number;
  speed_recv: number;
}

export interface HostStats {
  hostname: string;
  os: string;
  platform: string;
  platform_version: string;
  uptime: number;
  boot_time: number;
}

export interface SystemStats {
  cpu: CPUStats;
  memory: MemoryStats;
  disk: DiskStats;
  network: NetworkStats;
  host: HostStats;
}

// 监控记录（数据库保存的历史记录）
export interface MonitoringRecord {
  id: number;
  created_at: string;
  cpu_usage_percent: number;
  cpu_cores: number;
  memory_total: number;
  memory_used: number;
  memory_available: number;
  memory_used_percent: number;
  disk_total: number;
  disk_used: number;
  disk_free: number;
  disk_used_percent: number;
  network_bytes_sent: number;
  network_bytes_recv: number;
  network_packets_sent: number;
  network_packets_recv: number;
  network_speed_sent: number;
  network_speed_recv: number;
  hostname: string;
  uptime: number;
}

// 历史监控数据响应
export interface MonitoringHistoryResponse {
  records: MonitoringRecord[];
  count: number;
}

// 图表数据点（简化版，仅用于图表渲染）
export interface ChartDataPoint {
  timestamp: number;           // Unix timestamp
  cpu_percent: number;         // CPU usage percentage
  memory_percent: number;      // Memory usage percentage
  disk_percent: number;        // Disk usage percentage
  network_speed_sent: number;  // Network upload speed (bytes/s)
  network_speed_recv: number;  // Network download speed (bytes/s)
}

// 图表数据响应
export interface MonitoringChartResponse {
  points: ChartDataPoint[];
  count: number;
  period: {
    hours: number;
    from: number;
    to: number;
  };
}

// 监控统计数据响应
export interface MonitoringStatsResponse {
  records: MonitoringRecord[];
  stats: {
    count: number;
    period: {
      hours: number;
      from: number;
      to: number;
    };
    cpu: {
      average: number;
      max: number;
    };
    memory: {
      average: number;
      max: number;
    };
    disk: {
      average: number;
      max: number;
    };
  };
}
