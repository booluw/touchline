template>
  <div class="min-h-screen flex items-center justify-center">
    <main class="max-w-4xl w-full mx-auto px-6 py-16 text-center">
      <h1 class="text-5xl font-bold text-white mb-4">Touchline</h1>
      <p class="text-lg text-slate-400 mb-8">
        The persistent multiplayer football universe.
      </p>
      <p class="text-slate-500 max-w-2xl mx-auto">
        Every club has a personality. Every player has a story. Every decision has consequences.
      </p>
      <div class="mt-10">
        <PhaseZeroStatus />
      </div>

      <section v-if="loaded" class="mt-10 text-left space-y-6">
        <div
          v-for="section in sections"
          :key="section.key"
          class="space-y-3"
        >
          <h2 class="text-sm font-semibold uppercase tracking-wide" :class="section.headingClass">
            {{ section.label }}
          </h2>
          <p v-if="data[section.key].length === 0" class="text-slate-600 text-sm">
            Nothing here right now.
          </p>
          <ul class="space-y-2">
            <li
              v-for="item in data[section.key]"
              :key="item.id"
              class="rounded-lg bg-slate-900 border border-slate-700 p-4"
            >
              <p class="text-slate-100 font-medium">{{ item.title }}</p>
              <p class="text-slate-400 text-sm mt-1">{{ item.description }}</p>
              <span class="inline-block mt-2 text-xs text-slate-600">{{ item.category }}</span>
            </li>
          </ul>
        </div>
      </section>
      <p v-else-if="loading" class="mt-10 text-slate-500 text-sm">Loading your dashboard…</p>
    </main>
  </div>
</template>

<script setup lang="ts">
const { data, loading, loaded, fetchDashboard } = useDashboard()

const sections = [
  { key: 'urgent' as const, label: 'Urgent', headingClass: 'text-red-400' },
  { key: 'important' as const, label: 'Important', headingClass: 'text-amber-400' },
  { key: 'interesting' as const, label: 'Interesting', headingClass: 'text-slate-400' },
]

onMounted(fetchDashboard)
</script>