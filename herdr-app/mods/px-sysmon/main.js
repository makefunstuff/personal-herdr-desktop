// CPU, GPU (NVIDIA via nvidia-smi, AMD and Intel via sysfs), RAM and local disk use, for the sidebar. One shell call every few seconds.

import { View, div } from "gpui-kit";
import { v_flex, h_flex } from "gpui-base";
import { exec, theme } from "herdr";

const EVERY = 3000;
const MAX_DISKS = 4;

// One line per reading: "cpu …" (/proc/stat), Mem*, "df …" (local disks), "gpu …" (NVIDIA), "amd …" (sysfs).
const SAMPLE = `
[ -r /proc/stat ] && head -1 /proc/stat
grep -E '^(MemTotal|MemAvailable):' /proc/meminfo 2>/dev/null
df -P -l -x tmpfs -x devtmpfs -x efivarfs -x overlay -x squashfs 2>/dev/null | tail -n +2 | sed 's/^/df /'
command -v nvidia-smi >/dev/null && nvidia-smi --query-gpu=index,utilization.gpu,memory.used,memory.total,temperature.gpu --format=csv,noheader,nounits 2>/dev/null | grep -E '^[0-9]' | sed 's/^/gpu /'
for f in /sys/class/drm/card[0-9]/device/gpu_busy_percent; do [ -r "$f" ] && echo "amd \${f#/sys/class/drm/card} $(cat "$f")"; done
for c in /sys/class/drm/card[0-9]; do r="$c/gt/gt0/rc6_residency_ms"; [ -r "$r" ] && [ ! -r "$c/device/gpu_busy_percent" ] && echo "intel \${c#/sys/class/drm/card} $(cat "$r") $(date +%s%3N) $(cat "$c/device/device")"; done
`;

// Mounts nobody watches: boot partitions and system or container internals.
const SKIP = /^\/(boot|efi|run|snap|var\/lib|nix\/store|System\/Volumes)(\/|$)/;

export default class SysMon extends View {
  init(_props, cx) {
    this.prev = null;
    this.s = null;
    cx.timer.every(EVERY, (cx) => this.sample(cx));
    this.sample(cx);
  }

  sample(cx) {
    cx.spawn(async (cx) => {
      let out;
      try {
        out = (await exec("sh", ["-c", SAMPLE])).stdout;
      } catch {
        return;
      }
      const s = { gpus: [], disks: [] };
      const mem = {};
      const seen = new Set();
      for (const line of out.split("\n")) {
        const f = line.trim().split(/[\s,]+/);
        if (f[0] === "cpu") {
          const n = f.slice(1).map(Number);
          const idle = n[3] + n[4];
          const total = n.reduce((a, b) => a + b, 0);
          if (this.prev) s.cpu = 100 * (1 - (idle - this.prev.idle) / Math.max(1, total - this.prev.total));
          this.prev = { idle, total };
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
      s.linux = this.prev != null;
      s.ramUsed = (mem["MemTotal:"] - mem["MemAvailable:"]) * 1024;
      s.ramTotal = mem["MemTotal:"] * 1024;
      this.s = s;
      cx.notify();
    });
  }

  color(pct) {
    const t = theme();
    return pct >= 90 ? t.blocked : pct >= 70 ? t.working : t.done;
  }

  // One row: label, bar, percentage.
  meter(id, label, pct, tip) {
    const t = theme();
    const p = Math.min(100, Math.max(0, pct ?? 0));
    return h_flex().id(id).gap(8).items_center().tooltip(tip ?? label)
      .child(div().w(48).flex_none().truncate().text_color(t.muted).child(label))
      .child(div().flex_1().h(6).bg(t.raised).border(1).border_color(t.border)
        .child(div().h_full().w(`${p}%`).bg(this.color(p))))
      .child(div().w(34).flex_none().text_color(this.color(p)).child(pct == null ? "–" : `${Math.round(p)}%`));
  }

  render() {
    const t = theme();
    const s = this.s;
    const gb = (b) => `${(b / 2 ** 30).toFixed(1)}G`;
    return v_flex().size_full().min_h(0).bg(t.chrome).border_t(1).border_color(t.border)
      .child(h_flex().px(16).h(41).pt(14).pb(5).flex_none().items_center()
        .child(div().text_size(11.5).font_medium().text_color(t.dim).child("System")))
      .child(v_flex().id("sys-list").flex_1().min_h(0).overflow_y_scrollbar().px(16).pb(10).gap(7).text_size(11)
        .when(Boolean(s), (el) => el
          .when(!s.linux, (el) => el.child(div().text_color(t.dim).child("CPU, GPU and RAM: Linux only")))
          .when(s.linux, (el) => el.child(this.meter("cpu", "CPU", s.cpu)))
          .children(s.gpus.map((g) => this.meter(`gpu${g.index}`, s.gpus.length > 1 ? `GPU${g.index}` : "GPU", g.util,
            g.vram == null ? `GPU ${g.index}: ${g.util}% load` : `GPU ${g.index}: ${g.util}% load · VRAM ${Math.round(g.vram)}% · ${g.temp}°C`)))
          .when(s.ram != null, (el) => el.child(this.meter("ram", "RAM", s.ram, `RAM ${gb(s.ramUsed)} / ${gb(s.ramTotal)}`)))
          .children(s.disks.map((d) => this.meter(`disk${d.mount}`, d.mount, d.pct, `${d.mount}: ${gb(d.free)} free`)))));
  }
}
