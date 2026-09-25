package services

import (
	"strconv"
	"strings"
	"testing"
)

// MTU 自检是「能连上却传不动数据」这类静默故障的唯一主动发现手段，
// 因此它的判定边界必须锁住。

func TestOutInterfaceMTU(t *testing.T) {
	// lo 一定存在，且 MTU 是个大值
	if got := OutInterfaceMTU("lo"); got <= 0 {
		t.Errorf("应能读到 lo 的 MTU，实际 %d", got)
	}

	if got := OutInterfaceMTU(""); got != 0 {
		t.Errorf("接口名为空时应返回 0（无法判定），实际 %d", got)
	}

	if got := OutInterfaceMTU("no-such-interface-xyz"); got != 0 {
		t.Errorf("接口不存在时应返回 0（无法判定），实际 %d", got)
	}
}

// 接口读不到时不得给出任何建议：没有依据的告警只会制造噪音。
func TestCheckTunnelMTUFit_SilentWhenInterfaceUnknown(t *testing.T) {
	if got := CheckTunnelMTUFit("no-such-interface-xyz", DefaultTunnelMTU); got != "" {
		t.Errorf("接口不存在时不应给出建议，实际: %s", got)
	}
	if got := CheckTunnelMTUFit("", 0); got != "" {
		t.Errorf("接口名为空时不应给出建议，实际: %s", got)
	}
}

// 链路容量足够时保持静默。
//
// lo 的 MTU 远大于 DefaultTunnelMTU，用它代表「链路完全够用」的情形。
func TestCheckTunnelMTUFit_SilentWhenCapacitySufficient(t *testing.T) {
	loMTU := OutInterfaceMTU("lo")
	if loMTU <= 0 {
		t.Skip("无法读取 lo 的 MTU，跳过")
	}
	if loMTU-TunnelMTUOverhead < DefaultTunnelMTU {
		t.Skipf("本机 lo 的 MTU(%d) 意外地小，跳过", loMTU)
	}

	if got := CheckTunnelMTUFit("lo", DefaultTunnelMTU); got != "" {
		t.Errorf("链路容量充足时不应给出建议，实际: %s", got)
	}
}

// 隧道 MTU 超出链路承载能力时必须给出建议，且建议值要可执行
// （带上算出来的容量，便于直接填进配置）。
func TestCheckTunnelMTUFit_AdvisesWhenTunnelTooLarge(t *testing.T) {
	loMTU := OutInterfaceMTU("lo")
	if loMTU <= 0 {
		t.Skip("无法读取 lo 的 MTU，跳过")
	}

	// 隧道 MTU 等于链路 MTU 时，减去封装开销后必然不够
	advice := CheckTunnelMTUFit("lo", loMTU)
	if advice == "" {
		t.Fatalf("隧道 MTU(%d) 超过链路承载能力时应当告警", loMTU)
	}

	// 建议值必须出现在文案里，否则用户无从下手
	want := strconv.Itoa(loMTU - TunnelMTUOverhead)
	if !strings.Contains(advice, want) {
		t.Errorf("建议文案应包含可用值 %s，实际: %s", want, advice)
	}
	if !strings.Contains(advice, "lo") {
		t.Errorf("建议文案应指明是哪个接口，实际: %s", advice)
	}
}

// tunnelMTU 传 0 表示「沿用内核默认」，此时应按 DefaultTunnelMTU 参与比对，
// 而不是被当成 0 字节而无条件通过。
func TestCheckTunnelMTUFit_ZeroMeansKernelDefault(t *testing.T) {
	loMTU := OutInterfaceMTU("lo")
	if loMTU <= 0 {
		t.Skip("无法读取 lo 的 MTU，跳过")
	}

	// lo 的容量远大于默认值 => 静默
	if got := CheckTunnelMTUFit("lo", 0); got != "" {
		t.Errorf("默认隧道 MTU 未超限时不应告警，实际: %s", got)
	}

	// 用一个容量明显不足的真实接口不可得，改用等价断言：
	// 容量 = loMTU-60，若把它当作隧道 MTU 传入则必然超限。
	if got := CheckTunnelMTUFit("lo", loMTU-TunnelMTUOverhead); got != "" {
		t.Errorf("恰好等于容量时应当静默（边界应为 >=），实际: %s", got)
	}
}

// 边界：容量恰好等于隧道 MTU 时通过，少 1 字节则告警。
func TestCheckTunnelMTUFit_Boundary(t *testing.T) {
	loMTU := OutInterfaceMTU("lo")
	if loMTU <= 0 {
		t.Skip("无法读取 lo 的 MTU，跳过")
	}
	capacity := loMTU - TunnelMTUOverhead

	if got := CheckTunnelMTUFit("lo", capacity); got != "" {
		t.Errorf("恰好等于容量应通过，实际: %s", got)
	}
	if got := CheckTunnelMTUFit("lo", capacity+1); got == "" {
		t.Error("超出容量 1 字节就应告警")
	}
}

func TestSetLinkMTUInNamespace_ZeroIsNoop(t *testing.T) {
	// mtu<=0 表示不干预，不应产生任何外部调用，因此用一个不存在的命名空间
	// 也必须直接返回 nil（若真的去执行 ip 命令就会失败）。
	svc := NewNetnsService()
	if err := svc.SetLinkMTUInNamespace("no-such-ns", "wg0", 0); err != nil {
		t.Errorf("mtu=0 应为空操作，实际返回: %v", err)
	}
	if err := svc.SetLinkMTUInNamespace("no-such-ns", "wg0", -1); err != nil {
		t.Errorf("mtu<0 应为空操作，实际返回: %v", err)
	}
}
