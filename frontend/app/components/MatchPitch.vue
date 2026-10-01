<!--
  MatchPitch.vue — the IM34 2D match view. Draws the pitch, 22 players and the
  ball on a canvas from the simulation track (keyframes every 5 match seconds)
  and interpolates between keyframes at `playhead` (match milliseconds from
  kick-off). It only reads the track format, so a 3D view can be a second
  component over the same data. The text feed beside it is the accessible
  equivalent; the canvas carries a short label.
-->
<script setup lang="ts">
import type { TrackFrame, TrackMinute } from '~/stores/match'

const props = defineProps<{
  track: Record<number, TrackMinute>
  players: Record<string, string>
  playhead: number
  label: string
}>()

const FRAME_MS = 5000
const canvas = ref<HTMLCanvasElement | null>(null)

// frameAt returns the keyframe at index i of minute m, stepping into the next
// minute when i runs past the end.
function frameAt(m: number, i: number): { minute: TrackMinute; frame: TrackFrame } | null {
  let minute = props.track[m]
  if (minute && i >= minute.frames.length) {
    minute = props.track[m + 1]
    i = 0
  }
  const frame = minute?.frames[i]
  return minute && frame ? { minute, frame } : null
}

function draw() {
  const el = canvas.value
  const ctx = el?.getContext('2d')
  if (!el || !ctx) return
  const dpr = window.devicePixelRatio || 1
  const w = el.clientWidth
  const h = el.clientHeight
  if (el.width !== w * dpr || el.height !== h * dpr) {
    el.width = w * dpr
    el.height = h * dpr
  }
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0)

  const pad = 12
  const pw = w - pad * 2
  const ph = h - pad * 2
  const X = (v: number) => pad + (v / 1000) * pw
  const Y = (v: number) => pad + (v / 1000) * ph

  // Pitch and markings.
  ctx.fillStyle = '#166534'
  ctx.fillRect(0, 0, w, h)
  ctx.strokeStyle = 'rgba(255,255,255,0.7)'
  ctx.lineWidth = 1.5
  ctx.strokeRect(pad, pad, pw, ph)
  ctx.beginPath()
  ctx.moveTo(X(500), Y(0))
  ctx.lineTo(X(500), Y(1000))
  ctx.stroke()
  ctx.beginPath()
  ctx.arc(X(500), Y(500), ph * 0.13, 0, Math.PI * 2)
  ctx.stroke()
  for (const left of [true, false]) {
    const x0 = left ? 0 : 1000
    const dir = left ? 1 : -1
    ctx.strokeRect(X(x0), Y(210), dir * (165 / 1000) * pw, Y(790) - Y(210)) // penalty area
    ctx.strokeRect(X(x0), Y(370), dir * (55 / 1000) * pw, Y(630) - Y(370)) // goal area
    ctx.strokeRect(X(x0), Y(445), -dir * 6, Y(555) - Y(445)) // goal
  }

  const m = Math.floor(props.playhead / 60000) + 1
  const t = props.playhead % 60000
  const i = Math.floor(t / FRAME_MS)
  const a = frameAt(m, i)
  if (!a) return
  const b = frameAt(m, i + 1) ?? a
  const k = (t - i * FRAME_MS) / FRAME_MS
  const mix = (p: number, q: number) => p + (q - p) * k

  ctx.font = '10px sans-serif'
  ctx.textAlign = 'center'
  a.frame.p.forEach((p, slot) => {
    if (p[0] < 0) return
    const q = b.frame.p[slot]
    const to = q && q[0] >= 0 ? q : p
    const x = X(mix(p[0], to[0]))
    const y = Y(mix(p[1], to[1]))
    ctx.beginPath()
    ctx.arc(x, y, 6, 0, Math.PI * 2)
    ctx.fillStyle = slot < 11 ? '#818cf8' : '#fbbf24'
    ctx.fill()
    ctx.strokeStyle = '#0f172a'
    ctx.lineWidth = 1
    ctx.stroke()
    const name = props.players[a.minute.lineup[slot] ?? '']
    if (name) {
      ctx.fillStyle = 'rgba(255,255,255,0.85)'
      ctx.fillText(name.split(' ').pop() ?? '', x, y + 17)
    }
  })

  ctx.beginPath()
  ctx.arc(X(mix(a.frame.b[0], b.frame.b[0])), Y(mix(a.frame.b[1], b.frame.b[1])), 3.5, 0, Math.PI * 2)
  ctx.fillStyle = '#ffffff'
  ctx.fill()
  ctx.strokeStyle = '#0f172a'
  ctx.stroke()
}

watch(() => [props.playhead, props.track], draw, { deep: false })
onMounted(() => {
  draw()
  window.addEventListener('resize', draw)
})
onUnmounted(() => window.removeEventListener('resize', draw))
</script>

<template>
  <canvas ref="canvas" role="img" :aria-label="label" class="block w-full rounded-lg" style="aspect-ratio: 105 / 68" />
</template>
