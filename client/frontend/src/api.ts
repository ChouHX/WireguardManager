export interface Profile {
  id: string;
  name: string;
  endpoint: string;
  address: string;
  deviceLANs: string;
  tunnelNetwork: string;
  targets: string;
  adapterID: string;
}
export interface Adapter {
  id: string;
  name: string;
  addresses: string[];
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
}
interface API {
  Profiles(): Promise<Profile[]>;
  ImportConfig(): Promise<Profile[]>;
  SaveProfile(
    id: string,
    name: string,
    targets: string,
    adapterID: string,
  ): Promise<Profile>;
  RemoveProfile(id: string): Promise<void>;
  Adapters(): Promise<Adapter[]>;
  Connect(id: string): Promise<void>;
  Disconnect(): Promise<void>;
  Status(): Promise<Status>;
}
declare global {
  interface Window {
    go?: { main: { App: API } };
  }
}
// No configuration text or private key ever crosses this bridge.
export const api = (): API => {
  if (!window.go?.main?.App)
    throw new Error(
      "请在 Windows 桌面客户端中打开。浏览器预览不具备隧道控制能力。",
    );
  return window.go.main.App;
};
