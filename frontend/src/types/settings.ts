// 运行时配置（可在管理界面调整，持久化在数据库中）

export type SettingType = 'string' | 'int' | 'bool';

export type SettingGroup = 'network' | 'monitoring' | 'liveness' | 'auth' | 'wireguard';

export interface SettingDef {
  key: string;
  type: SettingType;
  group: SettingGroup;
  min?: number;
  max?: number;
}

export interface SettingsResponse {
  /** 当前值，键与 SettingDef.key 对应 */
  values: Record<string, string>;
  defs: SettingDef[];
}
