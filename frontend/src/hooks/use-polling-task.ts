import { useCallback, useRef } from 'react';

/**
 * 让轮询任务不重叠执行。
 *
 * 轮询间隔通常只有数秒，而单次请求可能因网络抖动耗时更久。不加约束时会出现两个问题：
 * 一是同一时刻堆积多个未完成请求；二是先发出但后返回的旧响应覆盖掉新数据，
 * 界面上表现为数据"退回到上一轮"。
 *
 * 这里在任务执行期间直接跳过后续触发，两个问题一并消除。
 */
export function usePollingTask(): (task: () => Promise<void>) => Promise<void> {
  const inFlight = useRef(false);

  return useCallback(async (task: () => Promise<void>) => {
    if (inFlight.current) return;
    inFlight.current = true;
    try {
      await task();
    } finally {
      inFlight.current = false;
    }
  }, []);
}
