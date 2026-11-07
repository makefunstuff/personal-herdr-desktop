// CPU, GPU (NVIDIA via nvidia-smi, AMD and Intel via sysfs; on macOS the Apple GPU via ioreg), RAM and local disk use, for
// the sidebar. One shell call every few seconds.

import { View, div } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { exec, theme, shared } from "herdr";

// The workbench layout writes its headings and labels in capitals (SPACES, AGENTS, BUS 1 · THIS COMPUTER).
const caps = (x) => (shared("layout") === "workbench" ? String(x).toUpperCase() : x);

const EVERY = 10000; // one sample (ps, vm_stat, df, ioreg) costs ~0.4 s of CPU on a Mac: every 10 s, not 3
const MAX_DISKS = 4;

// One line per reading: "cpu …" (/proc/stat), Mem*, "df …" (local disks), "gpu …" (NVIDIA), "amd …" (sysfs).
// macOS: "cpupct <sum of processes' %> <cores>" (ps), Mem* from sysctl and vm_stat (free, inactive, speculative and
// purgeable pages count as available), the Data volume as "/" and other local volumes (not Time Machine's
// snapshots), "mgpu 0 <busy %>" (ioreg's IOAccelerator).
const SAMPLE = `
if [ "$(uname)" = Darwin ]; then
  echo "cpupct $(ps -A -o %cpu= | awk '{s+=$1} END {print s+0}') $(/usr/sbin/sysctl -n hw.ncpu)"
  echo "MemTotal: $(( $(/usr/sbin/sysctl -n hw.memsize) / 1024 ))"
  vm_stat | awk '/page size of/ {ps=$8} /^Pages (free|inactive|speculative|purgeable):/ {a+=$NF+0} END {print "MemAvailable:", int(a*ps/1024)}'
  df -P -k /System/Volumes/Data 2>/dev/null | tail -n +2 | sed 's#/System/Volumes/Data$#/#; s/^/df /'
  df -P -l -k 2>/dev/null | tail -n +2 | grep -v '^com[.]apple' | grep ' /Volumes/' | sed 's/^/df /'
  /usr/sbin/ioreg -r -d 1 -c IOAccelerator 2>/dev/null | grep -o '"Device Utilization %"=[0-9]*' | head -1 | sed 's/.*=/mgpu 0 /'
  exit 0
fi
[ -r /proc/stat ] && head -1 /proc/stat
grep -E '^(MemTotal|MemAvailable):' /proc/meminfo 2>/dev/null
df -P -l -x tmpfs -x devtmpfs -x efivarfs -x overlay -x squashfs 2>/dev/null | tail -n +2 | sed 's/^/df /'
command -v nvidia-smi >/dev/null && nvidia-smi --query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu --format=csv,noheader,nounits 2>/dev/null | grep -E '^[0-9]' | sed 's/^/gpu /'
for f in /sys/class/drm/card[0-9]/device/gpu_busy_percent; do [ -r "$f" ] && echo "amd \${f#/sys/class/drm/card} $(cat "$f")"; done
for c in /sys/class/drm/card[0-9]; do r="$c/gt/gt0/rc6_residency_ms"; [ -r "$r" ] && [ ! -r "$c/device/gpu_busy_percent" ] && echo "intel \${c#/sys/class/drm/card} $(cat "$r") $(date +%s%3N) $(cat "$c/device/device")"; done
true
`;

// Why a reading failed, in words: Herdr gives a mod's runtime room for a fixed number of outstanding
// host tasks, and after many reloads of many mods it can run out — then no timer or call will start.
const whyNot = (e) => {
  const m = String(e?.message ?? e ?? "").split("\n")[0].trim();
  return /outstanding host task/i.test(m) ? "Herdr is out of room for its mods: reopen the window" : m || "Couldn't read this computer";
};

// Mounts nobody watches: boot partitions and system or container internals.
const SKIP = /^\/(boot|efi|run|snap|var\/lib|nix\/store|System\/Volumes)(\/|$)/;

export default class SysMon extends View {
  init(_props, cx) {
    this.prev = null;
    this.s = null;
    this.error = null;
    this.sampling = false;
    // Without the timer the reading stands still; the block still draws with the reason, never nothing.
    try {
      cx.timer.every(EVERY, (cx) => this.sample(cx));
    } catch (e) {
      this.error = whyNot(e);
    }
    this.sample(cx);
  }

  sample(cx) {
    // One reading at a time: a slow call must not stack up behind the timer.
    if (this.sampling) return;
    this.sampling = true;
    try {
      this.read(cx);
    } catch (e) {
      this.sampling = false;
      this.error = whyNot(e);
    }
  }

  // The read itself, in its own task: a runtime that can't start one is reported, not thrown.
  read(cx) {
    cx.spawn(async (cx) => {
      let out;
      try {
        out = (await exec("sh", ["-c", SAMPLE])).stdout;
      } catch (e) {
        this.sampling = false;
        this.error = whyNot(e);
        cx.notify();
        return;
      }
      this.sampling = false;
      this.error = null;
      const s = { gpus: [], disks: [] };
      const mem = {};
      const seen = new Set();
      let sawCpu = false;
      for (const line of out.split("\n")) {
        const f = line.trim().split(/[\s,]+/);
        if (f[0] === "cpu") {
          sawCpu = true;
          const n = f.slice(1).map(Number);
          const idle = n[3] + n[4];
          const total = n.reduce((a, b) => a + b, 0);
          if (this.prev) s.cpu = 100 * (1 - (idle - this.prev.idle) / Math.max(1, total - this.prev.total));
          this.prev = { idle, total };
        } else if (f[0] === "cpupct") {
          s.cpu = Number(f[1]) / Math.max(1, Number(f[2]));
        } else if (f[0] === "mgpu") {
          s.gpus.push({ index: f[1], util: Number(f[2]) });
        } else if (f[0] === "MemTotal:" || f[0] === "MemAvailable:") {
          mem[f[0]] = Number(f[1]);
        } else if (f[0] === "df") {
          const mount = f.slice(6).join(" ");
          if (!SKIP.test(mount) && !seen.has(f[1])) {
            seen.add(f[1]);
            s.disks.push({ mount, pct: Number(f[5].replace("%", "")), free: Number(f[4]) * 1024 });
          }
        } else if (f[0] === "gpu") {
          s.gpus.push({ index: f[1], util: Number(f[2]), vram: (100 * Number(f[3])) / Number(f[4]), temp: Number(f[5]) });
        } else if (f[0] === "intel") {
          // Intel i915/xe: busy % = the share of wall time the GPU was out of RC6 (idle) since the last sample.
          const prev = this.rc6?.[f[1]], rc6 = Number(f[2]), at = Number(f[3]);
          (this.rc6 ??= {})[f[1]] = { rc6, at };
          if (prev && at > prev.at) s.gpus.push({ index: f[1], util: Math.round(Math.max(0, Math.min(100, 100 - (100 * (rc6 - prev.rc6)) / (at - prev.at)))), dev: f[4] });
        } else if (f[0] === "amd") {
          s.gpus.push({ index: f[1].split("/")[0], util: Number(f[2]) });
        }
      }
      if (mem["MemTotal:"]) s.ram = 100 * (1 - mem["MemAvailable:"] / mem["MemTotal:"]);
      s.disks = s.disks.sort((a, b) => a.mount.length - b.mount.length).slice(0, MAX_DISKS);
      s.linux = sawCpu || s.cpu != null; // a /proc/stat cpu line (Linux), or macOS's cpupct
      s.ramUsed = (mem["MemTotal:"] - mem["MemAvailable:"]) * 1024;
      s.ramTotal = mem["MemTotal:"] * 1024;
      this.s = s;
      cx.notify();
    });
  }

  // Grey while there's room; amber when busy, red when nearly full (the Monitor's meters).
  color(pct) {
    const t = theme();
    return pct >= 90 ? t.blocked : pct >= 70 ? t.working : t.muted;
  }
  tone(pct) {
    const t = theme();
    return pct >= 90 ? t.blocked : pct >= 70 ? t.working : t.foreground;
  }

  // One row: label, a segmented bar like the Monitor's (thin cells), percentage.
  meter(id, label, pct, tip) {
    const t = theme();
    const p = Math.min(100, Math.max(0, pct ?? 0)), cells = 20, on = Math.round((p / 100) * cells);
    return h_flex().id(id).gap(8).items_center().tooltip(tip ?? label)
      .child(div().w(48).flex_none().truncate().text_size(10.5).text_color(t.dim).child(caps(label)))
      .child(h_flex().flex_1().gap(1).children(Array.from({ length: cells }, (_, i) => div().flex_1().h(5).bg(i < on ? this.color(p) : t.raised))))
      .child(div().w(34).flex_none().text_color(this.tone(p)).child(pct == null ? "–" : `${Math.round(p)}%`));
  }

  render() {
    const t = theme();
    const s = this.s;
    const gb = (b) => `${(b / 2 ** 30).toFixed(1)}G`;
    return v_flex().size_full().min_h(0).bg(t.chrome).border_t(1).border_color(t.border)
      .child(h_flex().px(16).h(41).pt(14).pb(5).flex_none().items_center()
        .child(div().text_size(11.5).font_medium().text_color(t.dim).child(caps("System"))))
      .child(v_flex().id("sys-list").flex_1().min_h(0).overflow_y_scrollbar().px(16).pb(10).gap(7).text_size(11)
        .when(!s && Boolean(this.error), (el) => el.child(div().text_size(10.5).text_color(t.blocked).child(caps(this.error))))
        .when(Boolean(s), (el) => el
          .when(!s.linux, (el) => el.child(div().text_color(t.dim).child(caps("CPU, GPU and RAM: Linux only"))))
          .when(s.linux, (el) => el.child(this.meter("cpu", "CPU", s.cpu)))
          .children(s.gpus.map((g) => this.meter(`gpu${g.index}`, s.gpus.length > 1 ? `GPU${g.index}` : "GPU", g.util,
            g.vram == null ? `GPU ${g.index}: ${g.util}% load` : `GPU ${g.index}: ${g.util}% load · VRAM ${Math.round(g.vram)}% · ${g.temp}°C`)))
          .when(s.ram != null, (el) => el.child(this.meter("ram", "RAM", s.ram, `RAM ${gb(s.ramUsed)} / ${gb(s.ramTotal)}`)))
          .children(s.disks.map((d) => this.meter(`disk${d.mount}`, d.mount, d.pct, `${d.mount}: ${gb(d.free)} free`)))));
  }
}
