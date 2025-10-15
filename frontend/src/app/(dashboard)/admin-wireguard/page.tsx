"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import { 
  Loader2, 
  Network,
  Users,
  ArrowDownToLine,
  ArrowUpFromLine,
  Activity,
  MoreVertical,
  Trash2,
  Power,
  PowerOff,
  Gauge
} from "lucide-react";

import { ContentLayout } from "@/components/admin-panel/content-layout";
import {
  Breadcrumb,
  BreadcrumbItem,
  BreadcrumbLink,
  BreadcrumbList,
  BreadcrumbPage,
  BreadcrumbSeparator
} from "@/components/ui/breadcrumb";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  DropdownMenuSeparator,
} from "@/components/ui/dropdown-menu";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { useI18n } from "@/hooks/use-i18n";
import { WireguardService } from "@/services/wireguard";
import { AdminUserTraffic } from "@/types/wireguard";

export default function AdminWireguardPage() {
  const { t } = useI18n();
  const [allUsersTraffic, setAllUsersTraffic] = useState<AdminUserTraffic[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [toggleDialogOpen, setToggleDialogOpen] = useState(false);
  const [rateLimitDialogOpen, setRateLimitDialogOpen] = useState(false);
  const [selectedServer, setSelectedServer] = useState<AdminUserTraffic | null>(null);
  const [submitting, setSubmitting] = useState(false);
  
  const [rateLimitForm, setRateLimitForm] = useState({
    download_rate: 0,
    upload_rate: 0
  });

  useEffect(() => {
    loadData(true);
    
    // 设置定时轮询，每30秒更新一次状态（管理员页面数据量大，不宜频繁轮询）
    const interval = setInterval(() => {
      loadData(false); // 轮询时不显示loading状态
    }, 3000);
    
    // 清理定时器
    return () => clearInterval(interval);
  }, []);

  const loadData = async (showLoading: boolean = true) => {
    try {
      if (showLoading) {
        setLoading(true);
      }
      setError("");
      
      const response = await WireguardService.getAdminTraffic();
      
      if (response.success && response.data) {
        setAllUsersTraffic(response.data);
      }
      
      setLastUpdated(new Date());
    } catch (err: any) {
      setError(err.message || t('errors.somethingWrong'));
    } finally {
      if (showLoading) {
        setLoading(false);
      }
    }
  };

  const handleDeleteServer = async () => {
    if (!selectedServer) return;
    
    try {
      setSubmitting(true);
      setError("");
      
      const response = await WireguardService.deleteServer(selectedServer.server_id);
      if (response.success) {
        setAllUsersTraffic(allUsersTraffic.filter(s => s.server_id !== selectedServer.server_id));
        setDeleteDialogOpen(false);
        setSelectedServer(null);
      }
    } catch (err: any) {
      setError(err.message || t('wireguard.deleteServerFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleToggleServer = async () => {
    if (!selectedServer) return;
    
    try {
      setSubmitting(true);
      setError("");
      
      const newStatus = !selectedServer.enabled;
      const response = await WireguardService.toggleServer(selectedServer.server_id, newStatus);
      if (response.success) {
        setAllUsersTraffic(allUsersTraffic.map(s => 
          s.server_id === selectedServer.server_id 
            ? { ...s, enabled: newStatus }
            : s
        ));
        setToggleDialogOpen(false);
        setSelectedServer(null);
      }
    } catch (err: any) {
      setError(err.message || t(selectedServer.enabled ? 'wireguard.disableServerFailed' : 'wireguard.enableServerFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleSetRateLimit = async () => {
    if (!selectedServer) return;
    
    try {
      setSubmitting(true);
      setError("");
      
      const response = await WireguardService.setRateLimit(
        selectedServer.server_id,
        rateLimitForm.download_rate,
        rateLimitForm.upload_rate
      );
      if (response.success) {
        setAllUsersTraffic(allUsersTraffic.map(s => 
          s.server_id === selectedServer.server_id 
            ? { ...s, download_rate: rateLimitForm.download_rate, upload_rate: rateLimitForm.upload_rate }
            : s
        ));
        setRateLimitDialogOpen(false);
        setSelectedServer(null);
        setRateLimitForm({ download_rate: 0, upload_rate: 0 });
      }
    } catch (err: any) {
      setError(err.message || t('wireguard.rateLimitFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const formatBytes = (bytes: number) => {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return Math.round((bytes / Math.pow(k, i)) * 100) / 100 + ' ' + sizes[i];
  };

  const formatDate = (dateString?: string) => {
    if (!dateString) return t('wireguard.never');
    const date = new Date(dateString);
    const now = new Date();
    const diffMs = now.getTime() - date.getTime();
    const diffMins = Math.floor(diffMs / 60000);
    
    if (diffMins < 1) return t('wireguard.justNow');
    if (diffMins < 60) return `${diffMins} ${t('wireguard.minutesAgo')}`;
    const diffHours = Math.floor(diffMins / 60);
    if (diffHours < 24) return `${diffHours} ${t('wireguard.hoursAgo')}`;
    const diffDays = Math.floor(diffHours / 24);
    return `${diffDays} ${t('wireguard.daysAgo')}`;
  };

  const getTotalStats = () => {
    return allUsersTraffic.reduce(
      (acc, user) => ({
        totalUsers: acc.totalUsers + 1,
        totalPeers: acc.totalPeers + user.peer_count,
        totalRx: acc.totalRx + user.total_rx,
        totalTx: acc.totalTx + user.total_tx,
      }),
      { totalUsers: 0, totalPeers: 0, totalRx: 0, totalTx: 0 }
    );
  };

  const totalStats = getTotalStats();

  return (
    <ContentLayout title={t('wireguard.adminTitle')}>
      <Breadcrumb>
        <BreadcrumbList>
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link href="/">{t('breadcrumb.home')}</Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbLink asChild>
              <Link href="/dashboard">{t('breadcrumb.dashboard')}</Link>
            </BreadcrumbLink>
          </BreadcrumbItem>
          <BreadcrumbSeparator />
          <BreadcrumbItem>
            <BreadcrumbPage>{t('breadcrumb.adminWireguard')}</BreadcrumbPage>
          </BreadcrumbItem>
        </BreadcrumbList>
      </Breadcrumb>

      <div className="mt-6 flex items-center justify-between">
        {error && (
          <div className="text-sm text-red-600 bg-red-50 dark:bg-red-950/20 p-3 rounded-md flex-1">
            {error}
          </div>
        )}
        {lastUpdated && !loading && (
          <div className="text-xs text-muted-foreground flex items-center gap-2">
            <Activity className="h-3 w-3" />
            {t('wireguard.lastUpdated')}: {lastUpdated.toLocaleTimeString()}
          </div>
        )}
      </div>

      {loading ? (
        <div className="flex items-center justify-center py-12 mt-6">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      ) : (
        <>
          {/* Overall Statistics */}
          <div className="grid gap-6 mt-6 md:grid-cols-2 lg:grid-cols-4">
            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  {t('wireguard.totalUsers')}
                </CardTitle>
                <Users className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{totalStats.totalUsers}</div>
                <p className="text-xs text-muted-foreground">
                  {t('wireguard.activeUsers')}
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  {t('wireguard.totalPeers')}
                </CardTitle>
                <Network className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{totalStats.totalPeers}</div>
                <p className="text-xs text-muted-foreground">
                  {t('wireguard.allDevices')}
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  {t('wireguard.totalDownload')}
                </CardTitle>
                <ArrowDownToLine className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{formatBytes(totalStats.totalRx)}</div>
                <p className="text-xs text-muted-foreground">
                  {t('wireguard.allUsersReceived')}
                </p>
              </CardContent>
            </Card>

            <Card>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium">
                  {t('wireguard.totalUpload')}
                </CardTitle>
                <ArrowUpFromLine className="h-4 w-4 text-muted-foreground" />
              </CardHeader>
              <CardContent>
                <div className="text-2xl font-bold">{formatBytes(totalStats.totalTx)}</div>
                <p className="text-xs text-muted-foreground">
                  {t('wireguard.allUsersTransmitted')}
                </p>
              </CardContent>
            </Card>
          </div>

          {/* Users Traffic List */}
          <Card className="mt-6">
            <CardHeader>
              <CardTitle>{t('wireguard.usersTraffic')}</CardTitle>
              <CardDescription>{t('wireguard.monitorAllUsers')}</CardDescription>
            </CardHeader>
            <CardContent>
              {allUsersTraffic.length === 0 ? (
                <div className="text-center py-8 text-muted-foreground">
                  {t('wireguard.noUsersFound')}
                </div>
              ) : (
                <div className="rounded-md border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('wireguard.user')}</TableHead>
                        <TableHead>{t('wireguard.namespace')}</TableHead>
                        <TableHead>{t('wireguard.status')}</TableHead>
                        <TableHead>{t('wireguard.peers')}</TableHead>
                        <TableHead>{t('wireguard.download')}</TableHead>
                        <TableHead>{t('wireguard.upload')}</TableHead>
                        <TableHead>{t('wireguard.rateLimit')}</TableHead>
                        <TableHead className="text-right">{t('wireguard.actions')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {allUsersTraffic.map((userTraffic) => (
                        <TableRow key={userTraffic.user_id}>
                          <TableCell>
                            <div>
                              <div className="font-medium">{userTraffic.email}</div>
                              <div className="text-xs text-muted-foreground">
                                UID: {userTraffic.user_uid}
                              </div>
                            </div>
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            <div>
                              <div>{userTraffic.namespace}</div>
                              <div className="text-xs">
                                {userTraffic.wg_address}:{userTraffic.wg_port}
                              </div>
                            </div>
                          </TableCell>
                          <TableCell>
                            <Badge variant={userTraffic.enabled ? "default" : "secondary"}>
                              {userTraffic.enabled ? t('wireguard.enabled') : t('wireguard.disabled')}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            <div className="flex items-center gap-2">
                              <Network className="h-3 w-3 text-muted-foreground" />
                              <span className="font-medium">{userTraffic.peer_count}</span>
                            </div>
                          </TableCell>
                          <TableCell>
                            <div className="flex items-center gap-1 text-green-600">
                              <ArrowDownToLine className="h-3 w-3" />
                              <span className="text-sm">{formatBytes(userTraffic.total_rx)}</span>
                            </div>
                          </TableCell>
                          <TableCell>
                            <div className="flex items-center gap-1 text-blue-600">
                              <ArrowUpFromLine className="h-3 w-3" />
                              <span className="text-sm">{formatBytes(userTraffic.total_tx)}</span>
                            </div>
                          </TableCell>
                          <TableCell>
                            <div className="text-sm">
                              {userTraffic.download_rate > 0 || userTraffic.upload_rate > 0 ? (
                                <>
                                  <div>↓ {userTraffic.download_rate} Mbps</div>
                                  <div>↑ {userTraffic.upload_rate} Mbps</div>
                                </>
                              ) : (
                                <span className="text-muted-foreground">{t('wireguard.unlimited')}</span>
                              )}
                            </div>
                          </TableCell>
                          <TableCell className="text-right">
                            <DropdownMenu>
                              <DropdownMenuTrigger asChild>
                                <Button variant="ghost" size="icon">
                                  <MoreVertical className="h-4 w-4" />
                                </Button>
                              </DropdownMenuTrigger>
                              <DropdownMenuContent align="end">
                                <DropdownMenuItem onClick={() => {
                                  setSelectedServer(userTraffic);
                                  setToggleDialogOpen(true);
                                }}>
                                  {userTraffic.enabled ? (
                                    <>
                                      <PowerOff className="mr-2 h-4 w-4" />
                                      {t('wireguard.disable')}
                                    </>
                                  ) : (
                                    <>
                                      <Power className="mr-2 h-4 w-4" />
                                      {t('wireguard.enable')}
                                    </>
                                  )}
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => {
                                  setSelectedServer(userTraffic);
                                  setRateLimitForm({
                                    download_rate: userTraffic.download_rate,
                                    upload_rate: userTraffic.upload_rate
                                  });
                                  setRateLimitDialogOpen(true);
                                }}>
                                  <Gauge className="mr-2 h-4 w-4" />
                                  {t('wireguard.setRateLimit')}
                                </DropdownMenuItem>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem 
                                  onClick={() => {
                                    setSelectedServer(userTraffic);
                                    setDeleteDialogOpen(true);
                                  }}
                                  className="text-red-600 focus:text-red-600"
                                >
                                  <Trash2 className="mr-2 h-4 w-4" />
                                  {t('wireguard.deleteServer')}
                                </DropdownMenuItem>
                              </DropdownMenuContent>
                            </DropdownMenu>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}

      {/* Delete Server Dialog */}
      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('wireguard.deleteServer')}</DialogTitle>
            <DialogDescription>
              {t('wireguard.deleteServerConfirm')}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button variant="destructive" onClick={handleDeleteServer} disabled={submitting}>
              {submitting ? t('common.loading') : t('common.delete')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Toggle Server Dialog */}
      <Dialog open={toggleDialogOpen} onOpenChange={setToggleDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {selectedServer?.enabled ? t('wireguard.disableServer') : t('wireguard.enableServer')}
            </DialogTitle>
            <DialogDescription>
              {selectedServer?.enabled ? t('wireguard.disableServerConfirm') : t('wireguard.enableServerConfirm')}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setToggleDialogOpen(false)} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleToggleServer} disabled={submitting}>
              {submitting ? t('common.loading') : t('common.confirm')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Rate Limit Dialog */}
      <Dialog open={rateLimitDialogOpen} onOpenChange={setRateLimitDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('wireguard.setRateLimit')}</DialogTitle>
            <DialogDescription>
              {t('wireguard.rateLimitPlaceholder')}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="download_rate">{t('wireguard.downloadRate')} (Mbps)</Label>
              <Input
                id="download_rate"
                type="number"
                min="0"
                value={rateLimitForm.download_rate}
                onChange={(e) => setRateLimitForm({ ...rateLimitForm, download_rate: parseInt(e.target.value) || 0 })}
                placeholder="0 = unlimited"
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="upload_rate">{t('wireguard.uploadRate')} (Mbps)</Label>
              <Input
                id="upload_rate"
                type="number"
                min="0"
                value={rateLimitForm.upload_rate}
                onChange={(e) => setRateLimitForm({ ...rateLimitForm, upload_rate: parseInt(e.target.value) || 0 })}
                placeholder="0 = unlimited"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setRateLimitDialogOpen(false)} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleSetRateLimit} disabled={submitting}>
              {submitting ? t('common.loading') : t('common.save')}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </ContentLayout>
  );
}
