export interface User {
  id: number;
  name: string;
  email: string;
}
export interface Device {
  id: string;
  name: string;
  address: string;
  lans: string;
  targets: string;
  autoTargets: string;
}
export interface Desktop {
  serverURL: string;
  user: User | null;
  devices: Device[];
  message: string;
}
export interface Adapter {
  id: string;
  name: string;
  addresses: string[];
  autoEligible: boolean;
}
export interface Detection {
  suggestedLANs: string;
  adapters: Adapter[];
}
export interface Network {
  adapters: string[] | null;
  forwarding: boolean;
  firewall: boolean;
  nat: boolean;
  warning: string;
}
export interface Status {
  profileID: string;
  state: string;
  rxBytes: number;
  txBytes: number;
  rxBps: number;
  txBps: number;
  latencyMS: number;
  handshake: string;
  error: string;
  network: Network;
}
interface API {
  Bootstrap(): Promise<Desktop>;
  Login(email: string, password: string, remember: boolean): Promise<Desktop>;
  Logout(): Promise<Desktop>;
  Refresh(): Promise<Desktop>;
  DetectLANs(): Promise<Detection>;
  SaveDevice(id: string, lans: string, targets: string): Promise<Desktop>;
  Connect(id: string, lans: string, targets: string): Promise<Desktop>;
  Disconnect(): Promise<void>;
  Status(): Promise<Status>;
}
declare global {
  interface Window {
    go?: { main: { App: API } };
  }
}
// Tokens and WireGuard private keys stay in Go. All HTTP requests originate there.
export const api = (): API => {
  if (!window.go?.main?.App)
    throw new Error(
      "请在 Windows 桌面客户端中打开。浏览器预览不具备登录与隧道控制能力。",
    );
  return window.go.main.App;
};
