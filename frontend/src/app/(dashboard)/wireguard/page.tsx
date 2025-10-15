"use client";

import { useEffect, useState, useRef } from "react";
import { useI18n } from "@/hooks/use-i18n";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Button } from "@/components/ui/button";
import { WireguardService } from "@/services/wireguard";
import { 
  WireguardPeer, 
  AddPeerRequest, 
  UpdatePeerRequest,
  UserTrafficSummary
} from "@/types/wireguard";
import { 
  Network, 
  Plus, 
  Trash2, 
  Pencil, 
  ArrowDownToLine, 
  ArrowUpFromLine,
  Server,
  HardDrive,
  Clock,
  Loader2,
  Users,
  Globe,
  Download,
  Activity,
  MoreVertical,
  QrCode
} from "lucide-react";
import QRCode from "qrcode";
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
import { Switch } from "@/components/ui/switch";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Breadcrumb, BreadcrumbItem, BreadcrumbLink, BreadcrumbList, BreadcrumbPage, BreadcrumbSeparator } from "@/components/ui/breadcrumb";
import { InputTags } from "@/components/ui/input-tags";
import { isValidIPOrCIDR, normalizeIPToCIDR } from "@/lib/ip-validator";
import Link from "next/link";
import { ContentLayout } from "@/components/admin-panel/content-layout";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

export default function WireguardPage() {
  const { t } = useI18n();
  const [trafficSummary, setTrafficSummary] = useState<UserTrafficSummary | null>(null);
  const [peers, setPeers] = useState<WireguardPeer[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [lastUpdated, setLastUpdated] = useState<Date | null>(null);
  
  const [addDialogOpen, setAddDialogOpen] = useState(false);
  const [editDialogOpen, setEditDialogOpen] = useState(false);
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false);
  const [qrDialogOpen, setQrDialogOpen] = useState(false);
  const [selectedPeer, setSelectedPeer] = useState<WireguardPeer | null>(null);
  const [qrCodeDataUrl, setQrCodeDataUrl] = useState<string>("");
  const [loadingQr, setLoadingQr] = useState(false);
  
  const [allowedIPs, setAllowedIPs] = useState<string[]>([]);
  const [addForm, setAddForm] = useState({
    persistent_keepalive: 25,
    comment: "",
    enable_forwarding: false,
    forward_interface: ""
  });
  
  const [editAllowedIPs, setEditAllowedIPs] = useState<string[]>([]);
  const [editForm, setEditForm] = useState({
    persistent_keepalive: 0,
    comment: "",
    enable_forwarding: false,
    forward_interface: ""
  });
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    loadData(true);
    
    // 设置定时轮询，每3秒更新一次流量统计
    const interval = setInterval(() => {
      loadData(false);
    }, 3000);
    
    // 清理定时器
    return () => clearInterval(interval);
  }, []);

  // 加载数据
  const loadData = async (showLoading: boolean = true) => {
    try {
      if (showLoading) {
        setLoading(true);
      }
      setError("");
      
      // 只需要调用 traffic API，它已经包含了 peers 的流量信息
      const trafficResponse = await WireguardService.getMyTraffic();
      
      if (trafficResponse.success && trafficResponse.data) {
        setTrafficSummary(trafficResponse.data);
      }
      
      // 只在初始加载时获取完整的 peers 信息（用于显示配置）
      if (showLoading) {
        const peersResponse = await WireguardService.getMyPeers();
        if (peersResponse.success && peersResponse.data) {
          setPeers(peersResponse.data);
        }
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

  const handleAddPeer = async () => {
    try {
      setSubmitting(true);
      setError("");
      
      const requestData: AddPeerRequest = {
        allowed_ips: allowedIPs.length > 0 ? allowedIPs.join(", ") : undefined,
        persistent_keepalive: addForm.persistent_keepalive,
        comment: addForm.comment,
        enable_forwarding: addForm.enable_forwarding,
        forward_interface: addForm.enable_forwarding ? addForm.forward_interface : undefined
      };
      
      const response = await WireguardService.addPeer(requestData);
      if (response.success && response.data) {
        setPeers([...peers, response.data]);
        setAddDialogOpen(false);
        setAllowedIPs([]);
        setAddForm({
          persistent_keepalive: 25,
          comment: "",
          enable_forwarding: false,
          forward_interface: ""
        });
        await loadData(true); // Reload to get updated stats
      }
    } catch (err: any) {
      setError(err.message || t('wireguard.addPeerFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleEditPeer = async () => {
    if (!selectedPeer) return;
    
    try {
      setSubmitting(true);
      setError("");
      
      const requestData: UpdatePeerRequest = {
        allowed_ips: editAllowedIPs.length > 0 ? editAllowedIPs.join(", ") : undefined,
        persistent_keepalive: editForm.persistent_keepalive,
        comment: editForm.comment,
        enable_forwarding: editForm.enable_forwarding,
        forward_interface: editForm.enable_forwarding ? editForm.forward_interface : undefined
      };
      
      const response = await WireguardService.updatePeer(selectedPeer.id, requestData);
      if (response.success && response.data) {
        setPeers(peers.map(p => p.id === selectedPeer.id ? response.data! : p));
        setEditDialogOpen(false);
        setSelectedPeer(null);
        setEditAllowedIPs([]);
        setEditForm({
          persistent_keepalive: 0,
          comment: "",
          enable_forwarding: false,
          forward_interface: ""
        });
        await loadData(true); // Reload to get updated stats
      }
    } catch (err: any) {
      setError(err.message || t('wireguard.updatePeerFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDeletePeer = async () => {
    if (!selectedPeer) return;
    
    try {
      setSubmitting(true);
      setError("");
      
      const response = await WireguardService.deletePeer(selectedPeer.id);
      if (response.success) {
        setPeers(peers.filter(p => p.id !== selectedPeer.id));
        setDeleteDialogOpen(false);
        setSelectedPeer(null);
        await loadData(true); // Reload to get updated stats
      }
    } catch (err: any) {
      setError(err.message || t('wireguard.deletePeerFailed'));
    } finally {
      setSubmitting(false);
    }
  };

  const handleDownloadConfig = async (peer: WireguardPeer) => {
    try {
      await WireguardService.downloadPeerConfig(peer.id, peer.comment);
    } catch (err: any) {
      setError(err.message || t('wireguard.downloadConfigFailed'));
    }
  };

  const handleShowQrCode = async (peer: WireguardPeer) => {
    try {
      setLoadingQr(true);
      setSelectedPeer(peer);
      setQrDialogOpen(true);
      setQrCodeDataUrl("");
      
      const response = await WireguardService.getPeerConfig(peer.id);
      if (response.success && response.data) {
        // Generate QR code from config text
        const qrDataUrl = await QRCode.toDataURL(response.data.config, {
          width: 400,
          margin: 2,
          color: {
            dark: '#000000',
            light: '#FFFFFF'
          }
        });
        setQrCodeDataUrl(qrDataUrl);
      }
    } catch (err: any) {
      setError(err.message || t('wireguard.generateQrFailed'));
      setQrDialogOpen(false);
    } finally {
      setLoadingQr(false);
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

  const getPeerStats = (publicKey: string) => {
    return trafficSummary?.peers.find(p => p.public_key === publicKey);
  };

  return (
    <ContentLayout title={t('wireguard.title')}>
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
            <BreadcrumbPage>{t('breadcrumb.wireguard')}</BreadcrumbPage>
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
          {/* Server Info Card */}
          {trafficSummary && (
            <div className="grid gap-6 mt-6 md:grid-cols-3">
              <Card>
                <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                  <CardTitle className="text-sm font-medium">
                    {t('wireguard.totalPeers')}
                  </CardTitle>
                  <Users className="h-4 w-4 text-muted-foreground" />
                </CardHeader>
                <CardContent>
                  <div className="text-2xl font-bold">{trafficSummary.peer_count}</div>
                  <p className="text-xs text-muted-foreground">
                    {t('wireguard.connectedDevices')}
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
                  <div className="text-2xl font-bold">{formatBytes(trafficSummary.total_rx)}</div>
                  <p className="text-xs text-muted-foreground">
                    {t('wireguard.received')}
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
                  <div className="text-2xl font-bold">{formatBytes(trafficSummary.total_tx)}</div>
                  <p className="text-xs text-muted-foreground">
                    {t('wireguard.transmitted')}
                  </p>
                </CardContent>
              </Card>
            </div>
          )}

          {/* Peers Management Card */}
          <Card className="mt-6">
            <CardHeader>
              <div className="flex items-center justify-between">
                <div>
                  <CardTitle>{t('wireguard.myPeers')}</CardTitle>
                  <CardDescription>{t('wireguard.managePeers')}</CardDescription>
                </div>
                <Button onClick={() => setAddDialogOpen(true)}>
                  <Plus className="mr-2 h-4 w-4" />
                  {t('wireguard.addPeer')}
                </Button>
              </div>
            </CardHeader>
            <CardContent>
              {peers.length === 0 ? (
                <div className="text-center py-8 text-muted-foreground">
                  {t('wireguard.noPeers')}
                </div>
              ) : (
                <div className="rounded-md border">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead>{t('wireguard.device')}</TableHead>
                        <TableHead>{t('wireguard.peerAddress')}</TableHead>
                        <TableHead>{t('wireguard.allowedIPs')}</TableHead>
                        <TableHead>{t('wireguard.lastHandshake')}</TableHead>
                        <TableHead>{t('wireguard.transfer')}</TableHead>
                        <TableHead className="text-right">{t('common.actions')}</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {peers.map((peer) => {
                        const stats = getPeerStats(peer.public_key);
                        return (
                          <TableRow key={peer.id}>
                            <TableCell>
                              <div>
                                <div className="font-medium">{peer.comment || t('wireguard.unnamed')}</div>
                                <div className="text-xs text-muted-foreground truncate max-w-[200px]">
                                  {peer.public_key.substring(0, 20)}...
                                </div>
                              </div>
                            </TableCell>
                            <TableCell>
                              <Badge variant="secondary">{peer.peer_address}</Badge>
                            </TableCell>
                            <TableCell className="text-sm">
                              {peer.allowed_ips || t('wireguard.noAllowedIPs')}
                            </TableCell>
                            <TableCell>
                              <div className="flex items-center gap-2">
                                <Clock className="h-3 w-3 text-muted-foreground" />
                                <span className="text-sm">{formatDate(stats?.latest_handshake)}</span>
                              </div>
                            </TableCell>
                            <TableCell>
                              <div className="text-sm">
                                <div className="flex items-center gap-1">
                                  <ArrowDownToLine className="h-3 w-3 text-green-600" />
                                  <span>{formatBytes(stats?.transfer_rx || 0)}</span>
                                </div>
                                <div className="flex items-center gap-1">
                                  <ArrowUpFromLine className="h-3 w-3 text-blue-600" />
                                  <span>{formatBytes(stats?.transfer_tx || 0)}</span>
                                </div>
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
                                  <DropdownMenuItem onClick={() => handleShowQrCode(peer)}>
                                    <QrCode className="mr-2 h-4 w-4" />
                                    {t('wireguard.showQrCode')}
                                  </DropdownMenuItem>
                                  <DropdownMenuItem onClick={() => handleDownloadConfig(peer)}>
                                    <Download className="mr-2 h-4 w-4" />
                                    {t('wireguard.downloadConfig')}
                                  </DropdownMenuItem>
                                  <DropdownMenuItem onClick={() => {
                                    setSelectedPeer(peer);
                                    setEditAllowedIPs(peer.allowed_ips ? peer.allowed_ips.split(/,\s*/) : []);
                                    setEditForm({
                                      persistent_keepalive: peer.persistent_keepalive,
                                      comment: peer.comment || "",
                                      enable_forwarding: peer.enable_forwarding,
                                      forward_interface: peer.forward_interface || ""
                                    });
                                    setEditDialogOpen(true);
                                  }}>
                                    <Pencil className="mr-2 h-4 w-4" />
                                    {t('common.edit')}
                                  </DropdownMenuItem>
                                  <DropdownMenuItem 
                                    onClick={() => {
                                      setSelectedPeer(peer);
                                      setDeleteDialogOpen(true);
                                    }}
                                    className="text-red-600 focus:text-red-600"
                                  >
                                    <Trash2 className="mr-2 h-4 w-4" />
                                    {t('common.delete')}
                                  </DropdownMenuItem>
                                </DropdownMenuContent>
                              </DropdownMenu>
                            </TableCell>
                          </TableRow>
                        );
                      })}
                    </TableBody>
                  </Table>
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}

      {/* Add Peer Dialog */}
      <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t('wireguard.addPeer')}</DialogTitle>
            <DialogDescription>{t('wireguard.addPeerDescription')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="p-3 bg-blue-50 dark:bg-blue-950/20 rounded-md text-sm text-blue-800 dark:text-blue-300">
              {t('wireguard.autoGenerateNote')}
            </div>
            <div className="space-y-2">
              <Label htmlFor="add-allowed-ips">{t('wireguard.allowedIPs')}</Label>
              <InputTags
                value={allowedIPs}
                onChange={setAllowedIPs}
                placeholder="192.168.1.0/24, 10.0.0.1"
                validate={isValidIPOrCIDR}
                normalize={normalizeIPToCIDR}
                errorMessage={t('wireguard.invalidIPFormat')}
                disabled={submitting}
              />
              <p className="text-xs text-muted-foreground">{t('wireguard.allowedIPsHelp')}</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="add-keepalive">{t('wireguard.persistentKeepalive')}</Label>
              <Input
                id="add-keepalive"
                type="number"
                value={addForm.persistent_keepalive}
                onChange={(e) => setAddForm({ ...addForm, persistent_keepalive: parseInt(e.target.value) || 0 })}
                placeholder="25"
                disabled={submitting}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="add-comment">{t('wireguard.comment')}</Label>
              <Input
                id="add-comment"
                value={addForm.comment}
                onChange={(e) => setAddForm({ ...addForm, comment: e.target.value })}
                placeholder={t('wireguard.commentPlaceholder')}
                disabled={submitting}
              />
            </div>
            <div className="space-y-3 p-4 border rounded-lg">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label htmlFor="add-forwarding">{t('wireguard.enableForwarding')}</Label>
                  <p className="text-xs text-muted-foreground">{t('wireguard.forwardingHelp')}</p>
                </div>
                <Switch
                  id="add-forwarding"
                  checked={addForm.enable_forwarding}
                  onCheckedChange={(checked) => setAddForm({ ...addForm, enable_forwarding: checked })}
                  disabled={submitting}
                />
              </div>
              {addForm.enable_forwarding && (
                <div className="space-y-2 pt-2">
                  <Label htmlFor="add-forward-interface">{t('wireguard.forwardInterface')}</Label>
                  <Input
                    id="add-forward-interface"
                    value={addForm.forward_interface}
                    onChange={(e) => setAddForm({ ...addForm, forward_interface: e.target.value })}
                    placeholder={t('wireguard.forwardInterfacePlaceholder')}
                    disabled={submitting}
                  />
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddDialogOpen(false)} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleAddPeer} disabled={submitting}>
              {submitting ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  {t('common.loading')}
                </>
              ) : (
                t('common.submit')
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Peer Dialog */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('wireguard.editPeer')}</DialogTitle>
            <DialogDescription>{t('wireguard.editPeerDescription')}</DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="space-y-2">
              <Label htmlFor="edit-allowed-ips">{t('wireguard.allowedIPs')}</Label>
              <InputTags
                value={editAllowedIPs}
                onChange={setEditAllowedIPs}
                placeholder="192.168.1.0/24, 10.0.0.1"
                validate={isValidIPOrCIDR}
                normalize={normalizeIPToCIDR}
                errorMessage={t('wireguard.invalidIPFormat')}
                disabled={submitting}
              />
              <p className="text-xs text-muted-foreground">{t('wireguard.allowedIPsHelp')}</p>
            </div>
            <div className="space-y-2">
              <Label htmlFor="edit-keepalive">{t('wireguard.persistentKeepalive')}</Label>
              <Input
                id="edit-keepalive"
                type="number"
                value={editForm.persistent_keepalive || 0}
                onChange={(e) => setEditForm({ ...editForm, persistent_keepalive: parseInt(e.target.value) || 0 })}
                disabled={submitting}
              />
            </div>
            <div className="space-y-2">
              <Label htmlFor="edit-comment">{t('wireguard.comment')}</Label>
              <Input
                id="edit-comment"
                value={editForm.comment || ''}
                onChange={(e) => setEditForm({ ...editForm, comment: e.target.value })}
                disabled={submitting}
              />
            </div>
            <div className="space-y-3 p-4 border rounded-lg">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label htmlFor="edit-forwarding">{t('wireguard.enableForwarding')}</Label>
                  <p className="text-xs text-muted-foreground">{t('wireguard.forwardingHelp')}</p>
                </div>
                <Switch
                  id="edit-forwarding"
                  checked={editForm.enable_forwarding}
                  onCheckedChange={(checked) => setEditForm({ ...editForm, enable_forwarding: checked })}
                  disabled={submitting}
                />
              </div>
              {editForm.enable_forwarding && (
                <div className="space-y-2 pt-2">
                  <Label htmlFor="edit-forward-interface">{t('wireguard.forwardInterface')}</Label>
                  <Input
                    id="edit-forward-interface"
                    value={editForm.forward_interface}
                    onChange={(e) => setEditForm({ ...editForm, forward_interface: e.target.value })}
                    placeholder={t('wireguard.forwardInterfacePlaceholder')}
                    disabled={submitting}
                  />
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button onClick={handleEditPeer} disabled={submitting}>
              {submitting ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  {t('common.loading')}
                </>
              ) : (
                t('common.save')
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* QR Code Dialog */}
      <Dialog open={qrDialogOpen} onOpenChange={setQrDialogOpen}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{t('wireguard.qrCodeTitle')}</DialogTitle>
            <DialogDescription>{t('wireguard.qrCodeDescription')}</DialogDescription>
          </DialogHeader>
          <div className="flex flex-col items-center">
            {loadingQr ? (
              <div className="flex flex-col items-center gap-4">
                <Loader2 className="h-12 w-12 animate-spin text-muted-foreground" />
                <p className="text-sm text-muted-foreground">{t('wireguard.generatingQr')}</p>
              </div>
            ) : qrCodeDataUrl ? (
              <>
                <div className="bg-white p-4 rounded-lg shadow-sm">
                  <img src={qrCodeDataUrl} alt="WireGuard Config QR Code" className="w-full h-auto" />
                </div>
                {selectedPeer && (
                  <div className="mt-4 text-center">
                    <p className="text-sm font-medium">{selectedPeer.comment || t('wireguard.unnamed')}</p>
                    <p className="text-xs text-muted-foreground mt-1">{selectedPeer.peer_address}</p>
                  </div>
                )}
              </>
            ) : null}
          </div>
        </DialogContent>
      </Dialog>

      {/* Delete Peer Dialog */}
      <Dialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t('wireguard.deletePeer')}</DialogTitle>
            <DialogDescription>{t('wireguard.deletePeerConfirm')}</DialogDescription>
          </DialogHeader>
          {selectedPeer && (
            <div className="py-4">
              <p className="text-sm">
                <span className="font-medium">{t('wireguard.device')}:</span> {selectedPeer.comment || t('wireguard.unnamed')}
              </p>
              <p className="text-sm">
                <span className="font-medium">{t('wireguard.allowedIPs')}:</span> {selectedPeer.allowed_ips}
              </p>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteDialogOpen(false)} disabled={submitting}>
              {t('common.cancel')}
            </Button>
            <Button variant="destructive" onClick={handleDeletePeer} disabled={submitting}>
              {submitting ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  {t('common.loading')}
                </>
              ) : (
                t('common.delete')
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </ContentLayout>
  );
}
