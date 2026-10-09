export const formatBytes = (n: number) =>
  n < 1024
    ? `${n.toFixed(0)} B`
    : n < 1048576
      ? `${(n / 1024).toFixed(1)} KB`
      : `${(n / 1048576).toFixed(1)} MB`;
