import { useEffect, useRef } from 'react';

/**
 * 定时器 hook：callback 用 ref 保存，避免因闭包陈旧导致重复建立 interval。
 * delay 传 null 时暂停。
 */
export function useInterval(callback: () => void, delay: number | null) {
  const savedCallback = useRef(callback);

  useEffect(() => {
    savedCallback.current = callback;
  }, [callback]);

  useEffect(() => {
    if (delay === null) return;
    const id = window.setInterval(() => savedCallback.current(), delay);
    return () => window.clearInterval(id);
  }, [delay]);
}
