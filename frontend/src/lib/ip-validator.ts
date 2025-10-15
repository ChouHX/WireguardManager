/**
 * 验证 IP 地址格式（支持 IPv4 和 CIDR 表示法）
 */

// 验证 IPv4 地址
export function isValidIPv4(ip: string): boolean {
  const ipv4Regex = /^(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.(25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$/
  return ipv4Regex.test(ip)
}

// 验证 IPv6 地址
export function isValidIPv6(ip: string): boolean {
  const ipv6Regex = /^(([0-9a-fA-F]{1,4}:){7}[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,7}:|([0-9a-fA-F]{1,4}:){1,6}:[0-9a-fA-F]{1,4}|([0-9a-fA-F]{1,4}:){1,5}(:[0-9a-fA-F]{1,4}){1,2}|([0-9a-fA-F]{1,4}:){1,4}(:[0-9a-fA-F]{1,4}){1,3}|([0-9a-fA-F]{1,4}:){1,3}(:[0-9a-fA-F]{1,4}){1,4}|([0-9a-fA-F]{1,4}:){1,2}(:[0-9a-fA-F]{1,4}){1,5}|[0-9a-fA-F]{1,4}:((:[0-9a-fA-F]{1,4}){1,6})|:((:[0-9a-fA-F]{1,4}){1,7}|:)|fe80:(:[0-9a-fA-F]{0,4}){0,4}%[0-9a-zA-Z]{1,}|::(ffff(:0{1,4}){0,1}:){0,1}((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])|([0-9a-fA-F]{1,4}:){1,4}:((25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9])\.){3}(25[0-5]|(2[0-4]|1{0,1}[0-9]){0,1}[0-9]))$/
  return ipv6Regex.test(ip)
}

// 验证 CIDR 表示法（IPv4）
export function isValidIPv4CIDR(cidr: string): boolean {
  const parts = cidr.split('/')
  if (parts.length !== 2) return false
  
  const [ip, prefix] = parts
  if (!isValidIPv4(ip)) return false
  
  const prefixNum = parseInt(prefix, 10)
  return prefixNum >= 0 && prefixNum <= 32
}

// 验证 CIDR 表示法（IPv6）
export function isValidIPv6CIDR(cidr: string): boolean {
  const parts = cidr.split('/')
  if (parts.length !== 2) return false
  
  const [ip, prefix] = parts
  if (!isValidIPv6(ip)) return false
  
  const prefixNum = parseInt(prefix, 10)
  return prefixNum >= 0 && prefixNum <= 128
}

// 验证 IP 地址或 CIDR（支持 IPv4 和 IPv6）
export function isValidIPOrCIDR(value: string): boolean {
  // 检查是否包含 CIDR 表示法
  if (value.includes('/')) {
    return isValidIPv4CIDR(value) || isValidIPv6CIDR(value)
  }
  
  // 单个 IP 地址
  return isValidIPv4(value) || isValidIPv6(value)
}

// 规范化 IP 地址为 CIDR 格式
// 如果输入是单个 IP，自动添加 /32 (IPv4) 或 /128 (IPv6)
export function normalizeIPToCIDR(value: string): string {
  // 已经是 CIDR 格式
  if (value.includes('/')) {
    return value
  }
  
  // IPv4 地址，添加 /32
  if (isValidIPv4(value)) {
    return `${value}/32`
  }
  
  // IPv6 地址，添加 /128
  if (isValidIPv6(value)) {
    return `${value}/128`
  }
  
  // 无效格式，返回原值
  return value
}

// 验证多个 IP 地址或 CIDR（逗号分隔）
export function isValidIPList(value: string): boolean {
  if (!value.trim()) return false
  
  const items = value.split(',').map(item => item.trim())
  return items.every(item => isValidIPOrCIDR(item))
}

// 格式化错误消息
export function getIPValidationError(value: string): string {
  if (!value.trim()) {
    return "IP address or CIDR is required"
  }
  
  if (value.includes('/')) {
    const parts = value.split('/')
    if (parts.length !== 2) {
      return "Invalid CIDR format. Use format: IP/prefix (e.g., 192.168.1.0/24)"
    }
    
    const [ip, prefix] = parts
    if (!isValidIPv4(ip) && !isValidIPv6(ip)) {
      return "Invalid IP address in CIDR"
    }
    
    const prefixNum = parseInt(prefix, 10)
    if (isNaN(prefixNum)) {
      return "Prefix must be a number"
    }
    
    if (isValidIPv4(ip) && (prefixNum < 0 || prefixNum > 32)) {
      return "IPv4 prefix must be between 0 and 32"
    }
    
    if (isValidIPv6(ip) && (prefixNum < 0 || prefixNum > 128)) {
      return "IPv6 prefix must be between 0 and 128"
    }
  } else {
    if (!isValidIPv4(value) && !isValidIPv6(value)) {
      return "Invalid IP address format"
    }
  }
  
  return ""
}
