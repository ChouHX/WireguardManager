import { TagsInput } from "@mantine/core";

export const splitAddresses = (raw: string) =>
  raw.split(/[,;\s]+/).filter(Boolean);
export function normalizeAddress(raw: string): string | null {
  const match =
    /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})(?:\/(\d{1,2}))?$/.exec(raw);
  if (!match) return null;
  const octets = match.slice(1, 5).map(Number);
  const bits = match[5] === undefined ? 32 : Number(match[5]);
  if (
    octets.some((n) => n > 255) ||
    bits < 1 ||
    bits > 32 ||
    octets[0] === 127 ||
    octets[0] >= 224 ||
    octets.every((n) => n === 0)
  )
    return null;
  const ip = octets.reduce((sum, n) => (sum * 256 + n) >>> 0, 0);
  const network = (ip & (0xffffffff << (32 - bits))) >>> 0;
  return (
    [24, 16, 8, 0].map((shift) => (network >>> shift) & 255).join(".") +
    "/" +
    bits
  );
}
export function addressError(raw: string) {
  const values = splitAddresses(raw);
  if (values.length > 256) return "最多填写 256 个 IP / 网段";
  const invalid = values.find((value) => !normalizeAddress(value));
  return invalid ? `无效的 IPv4 地址或网段：${invalid}` : undefined;
}
export const addressDraft = (value: string, search: string) =>
  [
    ...new Set(
      splitAddresses(`${value},${search}`).map((v) => normalizeAddress(v) ?? v),
    ),
  ].join(", ");

export function AddressInput({
  id,
  descriptionID,
  value,
  search,
  onChange,
  onSearchChange,
  disabled,
  placeholder,
}: {
  id: string;
  descriptionID: string;
  value: string;
  search: string;
  onChange: (value: string) => void;
  onSearchChange: (value: string) => void;
  disabled: boolean;
  placeholder: string;
}) {
  return (
    <TagsInput
      id={id}
      aria-describedby={descriptionID}
      value={splitAddresses(value)}
      onChange={(values) => onChange(addressDraft(values.join(","), ""))}
      searchValue={search}
      onSearchChange={onSearchChange}
      splitChars={[",", ";", " ", "\n", "\t"]}
      acceptValueOnBlur
      clearable
      size="xs"
      placeholder={placeholder}
      disabled={disabled}
      error={addressError(value)}
      className="address-input"
    />
  );
}
