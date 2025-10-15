"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { Activity, Cpu, HardDrive, MemoryStick, Network, RefreshCw, Server, TrendingUp, TrendingDown } from "lucide-react";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { ContentLayout } from "@/components/admin-panel/content-layout";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator,
} from "@/components/ui/breadcrumb";
import { Button } from "@/components/ui/button";
import { Progress } from "@/components/ui/progress";
import { Badge } from "@/components/ui/badge";
import { LineChart, Line, AreaChart, Area, XAxis, YAxis, CartesianGrid, Tooltip, ResponsiveContainer } from "recharts";
import { MonitoringService } from "@/services/monitoring";
import { SystemStats } from "@/types/monitoring";
import { cn } from "@/lib/utils";
import { useI18n } from "@/hooks/use-i18n";

interface HistoryData {
  time: string;
  cpu: number;
  memory: number;
  networkSent: number;
  networkRecv: number;
}

export default function DashboardPage() {
  const { t } = useI18n();
  const [stats, setStats] = useState<SystemStats | null>(null);
  const [loading, setLoading] = useState(true);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date());
  const [history, setHistory] = useState<HistoryData[]>([]);

  // 加载历史数据（初次加载时调用）
  const loadHistoricalData = async () => {
    try {
      // 获取最近0.5小时的图表数据（简化版API）
      const response = await MonitoringService.getMonitoringChart(0.5);
      
      if (response.success && response.data && response.data.points.length > 0) {
        const points = response.data.points;
        
        // 转换为图表数据格式
        const historyData = points.map(point => ({
          time: new Date(point.timestamp * 1000).toLocaleTimeString('zh-CN', { 
            hour: '2-digit', 
            minute: '2-digit',
            second: '2-digit'
          }),
          cpu: point.cpu_percent,
          memory: point.memory_percent,
          networkSent: point.network_speed_sent / 1024 / 1024, // Convert to MB/s
          networkRecv: point.network_speed_recv / 1024 / 1024,
        }));
        
        setHistory(historyData);
      }
    } catch (error) {
      console.error("Failed to load historical data:", error);
    }
  };

  const fetchStats = async () => {
    try {
      const response = await MonitoringService.getSystemStats();

      if (response.success && response.data) {
        const newStats = response.data;
        setStats(newStats);
        setLastUpdate(new Date());

        // Add to history (keep last 60 data points for 30 minutes at 3s interval)
        setHistory((prev) => {
          const newHistory = [
            ...prev,
            {
              time: new Date().toLocaleTimeString('zh-CN', { 
                hour: '2-digit', 
                minute: '2-digit',
                second: '2-digit'
              }),
              cpu: newStats.cpu.usage_percent,
              memory: newStats.memory.used_percent,
              networkSent: newStats.network.speed_sent / 1024 / 1024, // Convert to MB/s
              networkRecv: newStats.network.speed_recv / 1024 / 1024,
            },
          ];
          return newHistory.slice(-60); // Keep last 60 points (3 minutes at 3s interval)
        });
      }
    } catch (error) {
      console.error("Failed to fetch system stats:", error);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    // 初次加载时先获取历史数据
    const initializeData = async () => {
      await loadHistoricalData();
      // 然后获取最新的实时数据
      await fetchStats();
    };
    
    initializeData();
  }, []);

  useEffect(() => {
    if (autoRefresh) {
      const interval = setInterval(fetchStats, 3000); // Refresh every 3 seconds
      return () => clearInterval(interval);
    }
  }, [autoRefresh]);

  const formatBytes = (bytes: number): string => {
    if (bytes === 0) return "0 B";
    const k = 1024;
    const sizes = ["B", "KB", "MB", "GB", "TB"];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return `${(bytes / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`;
  };

  const formatSpeed = (bytesPerSecond: number): string => {
    return `${formatBytes(bytesPerSecond)}/s`;
  };

  const formatUptime = (seconds: number): string => {
    const days = Math.floor(seconds / 86400);
    const hours = Math.floor((seconds % 86400) / 3600);
    const minutes = Math.floor((seconds % 3600) / 60);
    
    if (days > 0) {
      return `${days} ${t("monitoring.days")} ${hours} ${t("monitoring.hours")}`;
    } else if (hours > 0) {
      return `${hours} ${t("monitoring.hours")} ${minutes} ${t("monitoring.minutes")}`;
    } else {
      return `${minutes} ${t("monitoring.minutes")}`;
    }
  };

  const getStatusColor = (percent: number) => {
    if (percent >= 90) return "text-red-500";
    if (percent >= 70) return "text-yellow-500";
    return "text-green-500";
  };

  const getProgressColor = (percent: number) => {
    if (percent >= 90) return "bg-red-500";
    if (percent >= 70) return "bg-yellow-500";
    return "bg-green-500";
  };

  // Custom tooltip formatter for network speed
  const formatNetworkSpeed = (value: number) => {
    if (value === 0) return "0 B/s";
    const k = 1024;
    const sizes = ["B/s", "KB/s", "MB/s", "GB/s"];
    const i = Math.floor(Math.log(value * k * k) / Math.log(k));
    return `${((value * k * k) / Math.pow(k, i)).toFixed(2)} ${sizes[i]}`;
  };

  // Custom tooltip formatter for percentage
  const formatPercentage = (value: number) => {
    return `${value.toFixed(1)}%`;
  };

  if (loading || !stats) {
    return (
      <ContentLayout title={t("monitoring.title")}>
        <div className="flex items-center justify-center h-96">
          <div className="text-center">
            <RefreshCw className="h-8 w-8 animate-spin mx-auto mb-4 text-muted-foreground" />
            <p className="text-muted-foreground">{t("common.loading")}</p>
          </div>
        </div>
      </ContentLayout>
    );
  }

  return (
    <ContentLayout title={t("monitoring.title")}>
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link href="/">{t("breadcrumb.home")}</Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>{t("breadcrumb.dashboard")}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>

      <div className="space-y-4">
        {/* Compact Header */}
        <div className="flex items-center justify-between">
          <div>
            <h2 className="text-2xl font-bold tracking-tight">{t("monitoring.systemResources")}</h2>
            <p className="text-sm text-muted-foreground">{t("monitoring.monitorServer")}</p>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant="outline" className="text-xs">
              {lastUpdate.toLocaleTimeString()}
            </Badge>
            <Button
              variant={autoRefresh ? "default" : "outline"}
              size="sm"
              onClick={() => setAutoRefresh(!autoRefresh)}
            >
              <RefreshCw className={cn("h-3.5 w-3.5 mr-1.5", autoRefresh && "animate-spin")} />
              <span className="text-xs">{t("monitoring.auto")}</span>
            </Button>
            <Button variant="outline" size="sm" onClick={fetchStats}>
              <RefreshCw className="h-3.5 w-3.5" />
            </Button>
          </div>
        </div>

        {/* Resource Overview Cards with Progress Bars */}
        <div className="grid gap-3 md:grid-cols-2 lg:grid-cols-4">
          {/* CPU Card */}
          <Card className="overflow-hidden">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{t("monitoring.cpuUsage")}</CardTitle>
              <Cpu className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="flex items-baseline justify-between">
                <div className={cn("text-3xl font-bold", getStatusColor(stats.cpu.usage_percent))}>
                  {stats.cpu.usage_percent.toFixed(1)}%
                </div>
                <div className="flex items-center text-xs text-muted-foreground">
                  {stats.cpu.usage_percent > 50 ? (
                    <TrendingUp className="h-3 w-3 mr-1 text-red-500" />
                  ) : (
                    <TrendingDown className="h-3 w-3 mr-1 text-green-500" />
                  )}
                  {stats.cpu.cores} {t("monitoring.cpuCores")}
                </div>
              </div>
              <div className="space-y-1">
                <Progress 
                  value={stats.cpu.usage_percent} 
                  className="h-2"
                  style={{
                    // @ts-ignore
                    '--progress-background': getProgressColor(stats.cpu.usage_percent)
                  } as React.CSSProperties}
                />
                <p className="text-xs text-muted-foreground">
                  {stats.cpu.usage_percent < 70 ? t("monitoring.normal") : stats.cpu.usage_percent < 90 ? t("monitoring.high") : t("monitoring.critical")}
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Memory Card */}
          <Card className="overflow-hidden">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{t("monitoring.memoryUsage")}</CardTitle>
              <MemoryStick className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="flex items-baseline justify-between">
                <div className={cn("text-3xl font-bold", getStatusColor(stats.memory.used_percent))}>
                  {stats.memory.used_percent.toFixed(1)}%
                </div>
                <div className="text-xs text-muted-foreground">
                  {formatBytes(stats.memory.total)}
                </div>
              </div>
              <div className="space-y-1">
                <Progress 
                  value={stats.memory.used_percent} 
                  className="h-2"
                />
                <p className="text-xs text-muted-foreground">
                  {formatBytes(stats.memory.used)} / {formatBytes(stats.memory.total)}
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Disk Card */}
          <Card className="overflow-hidden">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{t("monitoring.diskUsage")}</CardTitle>
              <HardDrive className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="flex items-baseline justify-between">
                <div className={cn("text-3xl font-bold", getStatusColor(stats.disk.used_percent))}>
                  {stats.disk.used_percent.toFixed(1)}%
                </div>
                <div className="text-xs text-muted-foreground">
                  {formatBytes(stats.disk.total)}
                </div>
              </div>
              <div className="space-y-1">
                <Progress 
                  value={stats.disk.used_percent} 
                  className="h-2"
                />
                <p className="text-xs text-muted-foreground">
                  {formatBytes(stats.disk.used)} / {formatBytes(stats.disk.total)}
                </p>
              </div>
            </CardContent>
          </Card>

          {/* Network Card */}
          <Card className="overflow-hidden">
            <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
              <CardTitle className="text-sm font-medium">{t("monitoring.networkSpeed")}</CardTitle>
              <Network className="h-4 w-4 text-muted-foreground" />
            </CardHeader>
            <CardContent className="space-y-2">
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">{t("monitoring.uploadSpeed")}</span>
                  <span className="text-sm font-bold text-blue-500">
                    ↑ {formatSpeed(stats.network.speed_sent)}
                  </span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">{t("monitoring.downloadSpeed")}</span>
                  <span className="text-sm font-bold text-green-500">
                    ↓ {formatSpeed(stats.network.speed_recv)}
                  </span>
                </div>
              </div>
              <div className="pt-2 border-t space-y-1">
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">{t("monitoring.uploadTotal")}</span>
                  <span className="text-xs font-semibold">{formatBytes(stats.network.bytes_sent)}</span>
                </div>
                <div className="flex items-center justify-between">
                  <span className="text-xs text-muted-foreground">{t("monitoring.downloadTotal")}</span>
                  <span className="text-xs font-semibold">{formatBytes(stats.network.bytes_recv)}</span>
                </div>
              </div>
            </CardContent>
          </Card>
        </div>

        {/* Charts Section */}
        <div className="grid gap-3 md:grid-cols-2">
          {/* CPU & Memory Trend */}
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Activity className="h-4 w-4 text-purple-500" />
                  <CardTitle className="text-base">{t("monitoring.resourceUsage")}</CardTitle>
                </div>
                <Badge variant="secondary" className="text-xs">{t("monitoring.realtime")}</Badge>
              </div>
              <CardDescription className="text-xs">
                {t("monitoring.cpuAndMemory")}
              </CardDescription>
            </CardHeader>
            <CardContent className="pb-4">
              <ResponsiveContainer width="100%" height={220}>
                <AreaChart data={history} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
                  <defs>
                    <linearGradient id="colorCpu" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#8b5cf6" stopOpacity={0.4}/>
                      <stop offset="95%" stopColor="#8b5cf6" stopOpacity={0.05}/>
                    </linearGradient>
                    <linearGradient id="colorMemory" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#ec4899" stopOpacity={0.4}/>
                      <stop offset="95%" stopColor="#ec4899" stopOpacity={0.05}/>
                    </linearGradient>
                    <filter id="shadow" height="200%">
                      <feGaussianBlur in="SourceAlpha" stdDeviation="3"/>
                      <feOffset dx="0" dy="2" result="offsetblur"/>
                      <feComponentTransfer>
                        <feFuncA type="linear" slope="0.2"/>
                      </feComponentTransfer>
                      <feMerge>
                        <feMergeNode/>
                        <feMergeNode in="SourceGraphic"/>
                      </feMerge>
                    </filter>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted/30" vertical={false} />
                  <XAxis 
                    dataKey="time" 
                    tick={{ fill: 'hsl(var(--muted-foreground))', fontSize: 11 }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <YAxis 
                    tick={{ fill: 'hsl(var(--muted-foreground))', fontSize: 11 }}
                    tickLine={false}
                    axisLine={false}
                    domain={[0, 100]}
                  />
                  <Tooltip 
                    contentStyle={{ 
                      backgroundColor: 'hsl(var(--popover))',
                      border: '1px solid hsl(var(--border))',
                      borderRadius: '8px',
                      boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)',
                      padding: '8px 12px'
                    }}
                    labelStyle={{ fontSize: '12px', fontWeight: 600, marginBottom: '4px' }}
                    itemStyle={{ fontSize: '11px', padding: '2px 0' }}
                    formatter={(value: number, name: string) => [
                      formatPercentage(value),
                      name
                    ]}
                  />
                  <Area 
                    type="monotone" 
                    dataKey="cpu" 
                    stroke="#8b5cf6" 
                    fillOpacity={1}
                    fill="url(#colorCpu)" 
                    name={t("monitoring.cpuUsage")}
                    strokeWidth={2.5}
                    dot={false}
                    activeDot={{ r: 4, strokeWidth: 2, fill: '#8b5cf6' }}
                  />
                  <Area 
                    type="monotone" 
                    dataKey="memory" 
                    stroke="#ec4899" 
                    fillOpacity={1}
                    fill="url(#colorMemory)" 
                    name={t("monitoring.memoryUsage")}
                    strokeWidth={2.5}
                    dot={false}
                    activeDot={{ r: 4, strokeWidth: 2, fill: '#ec4899' }}
                  />
                </AreaChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>

          {/* Network Traffic */}
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Network className="h-4 w-4 text-blue-500" />
                  <CardTitle className="text-base">{t("monitoring.networkTraffic")}</CardTitle>
                </div>
                <Badge variant="secondary" className="text-xs">{t("monitoring.realtime")}</Badge>
              </div>
              <CardDescription className="text-xs">
                {t("monitoring.uploadAndDownload")}
              </CardDescription>
            </CardHeader>
            <CardContent className="pb-4">
              <ResponsiveContainer width="100%" height={220}>
                <LineChart data={history} margin={{ top: 5, right: 5, left: -20, bottom: 0 }}>
                  <defs>
                    <linearGradient id="colorUpload" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#3b82f6" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#3b82f6" stopOpacity={0}/>
                    </linearGradient>
                    <linearGradient id="colorDownload" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="#10b981" stopOpacity={0.3}/>
                      <stop offset="95%" stopColor="#10b981" stopOpacity={0}/>
                    </linearGradient>
                  </defs>
                  <CartesianGrid strokeDasharray="3 3" className="stroke-muted/30" vertical={false} />
                  <XAxis 
                    dataKey="time" 
                    tick={{ fill: 'hsl(var(--muted-foreground))', fontSize: 11 }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <YAxis 
                    tick={{ fill: 'hsl(var(--muted-foreground))', fontSize: 11 }}
                    tickLine={false}
                    axisLine={false}
                  />
                  <Tooltip 
                    contentStyle={{ 
                      backgroundColor: 'hsl(var(--popover))',
                      border: '1px solid hsl(var(--border))',
                      borderRadius: '8px',
                      boxShadow: '0 4px 6px -1px rgb(0 0 0 / 0.1)',
                      padding: '8px 12px'
                    }}
                    labelStyle={{ fontSize: '12px', fontWeight: 600, marginBottom: '4px' }}
                    itemStyle={{ fontSize: '11px', padding: '2px 0' }}
                    formatter={(value: number, name: string) => [
                      formatNetworkSpeed(value),
                      name
                    ]}
                  />
                  <Line 
                    type="monotone" 
                    dataKey="networkSent" 
                    stroke="#3b82f6" 
                    name={t("monitoring.uploadSpeed")}
                    strokeWidth={2.5}
                    dot={false}
                    activeDot={{ r: 4, strokeWidth: 2, fill: '#3b82f6' }}
                  />
                  <Line 
                    type="monotone" 
                    dataKey="networkRecv" 
                    stroke="#10b981" 
                    name={t("monitoring.downloadSpeed")}
                    strokeWidth={2.5}
                    dot={false}
                    activeDot={{ r: 4, strokeWidth: 2, fill: '#10b981' }}
                  />
                </LineChart>
              </ResponsiveContainer>
            </CardContent>
          </Card>
        </div>

        {/* System Information & CPU Cores */}
        <div className="grid gap-3 md:grid-cols-2">
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center gap-2">
                <Server className="h-4 w-4 text-green-500" />
                <CardTitle className="text-base">{t("monitoring.systemInfo")}</CardTitle>
              </div>
              <CardDescription className="text-xs">
                {t("monitoring.serverDetails")}
              </CardDescription>
            </CardHeader>
            <CardContent className="pb-4">
              <div className="grid gap-4 md:grid-cols-2">
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">{t("monitoring.hostname")}</p>
                  <p className="text-sm font-semibold">{stats.host.hostname}</p>
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">{t("monitoring.os")}</p>
                  <p className="text-sm font-semibold">{stats.host.os}</p>
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">{t("monitoring.platform")}</p>
                  <p className="text-sm font-semibold">
                    {stats.host.platform} {stats.host.platform_version}
                  </p>
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">{t("monitoring.uptime")}</p>
                  <p className="text-sm font-semibold">{formatUptime(stats.host.uptime)}</p>
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">{t("monitoring.bytesSent")}</p>
                  <p className="text-sm font-semibold">{formatBytes(stats.network.bytes_sent)}</p>
                </div>
                <div className="space-y-1">
                  <p className="text-xs font-medium text-muted-foreground">{t("monitoring.bytesRecv")}</p>
                  <p className="text-sm font-semibold">{formatBytes(stats.network.bytes_recv)}</p>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* CPU Cores Detail */}
          <Card>
            <CardHeader className="pb-3">
              <div className="flex items-center gap-2">
                <Cpu className="h-4 w-4 text-orange-500" />
                <CardTitle className="text-base">{t("monitoring.cpuCoresDetail")}</CardTitle>
              </div>
              <CardDescription className="text-xs">
                {stats.cpu.cores} {t("monitoring.coresUsage")}
              </CardDescription>
            </CardHeader>
            <CardContent className="pb-4">
              {stats.cpu.per_core && stats.cpu.per_core.length > 0 ? (
                <div className="grid gap-3 grid-cols-2">
                  {stats.cpu.per_core.map((usage, index) => (
                    <div key={index} className="space-y-1.5">
                      <div className="flex items-center justify-between">
                        <span className="text-xs font-medium">{t("monitoring.core")} {index + 1}</span>
                        <span className={cn("text-xs font-bold", getStatusColor(usage))}>
                          {usage.toFixed(1)}%
                        </span>
                      </div>
                      <Progress value={usage} className="h-1.5" />
                    </div>
                  ))}
                </div>
              ) : (
                <div className="flex items-center justify-center h-20 text-muted-foreground text-sm">
                  {t("common.loading")}...
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      </div>
    </ContentLayout>
  );
}
