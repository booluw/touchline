<script setup lang="ts">
import { useFinanceStore } from '~/app/stores/finance'
const store = useFinanceStore()
const clubId = ref('')
const error = ref('')
const usd = new Intl.NumberFormat('en-US', { style: 'currency', currency: 'USD', maximumFractionDigits: 0 })
const fmt = (v: number) => usd.format(v)
const sign = (v: number) => (v < 0 ? '−' : '') + fmt(Math.abs(v))
const factSum = () => store.summary?.factors.reduce((a, f) => a + f.amount, 0) ?? 0

async function init() {
  const { authedFetch } = useAuth()
  const r = await authedFetch('/api/clubs')
  const clubs = await r.json()
  clubId.value = clubs[0]?.id ?? ''
  if (!clubId.value) throw new Error('No club yet.')
  await store.load(clubId.value)
}
onMounted(() => init().catch(() => { error.value = 'Could not load your club finances.' }))
</script>

<template>
  <main class="min-h-screen bg-slate-900 px-6 py-10 text-slate-200">
    <div class="mx-auto max-w-5xl space-y-8">
      <h1 class="text-3xl font-bold text-white">Finances</h1>
      <p v-if="error" class="text-red-400">{{ error }}</p>
      <p v-if="store.loading && !store.summary" class="text-slate-400">Loading…</p>

      <template v-if="store.summary">
        <div class="grid gap-4 sm:grid-cols-3">
          <div class="rounded border border-slate-700 bg-slate-800 p-4">
            <span class="block text-sm text-slate-400">Cash balance</span>
            <strong class="text-2xl text-emerald-300">{{ fmt(store.summary.cash) }}</strong>
          </div>
          <div class="rounded border border-slate-700 bg-slate-800 p-4">
            <span class="block text-sm text-slate-400">Operating profit (season)</span>
            <strong class="text-2xl"
              :class="store.summary.operating_profit < 0 ? 'text-red-300' : 'text-emerald-300'">{{
                sign(store.summary.operating_profit) }}</strong>
          </div>
          <div class="rounded border border-slate-700 bg-slate-800 p-4">
            <span class="block text-sm text-slate-400">Projected year-end balance</span>
            <strong class="text-2xl"
              :class="store.summary.projected_year_end_balance < 0 ? 'text-red-300' : 'text-slate-100'">{{
                sign(store.summary.projected_year_end_balance) }}</strong>
          </div>
        </div>

        <section class="rounded border border-slate-700 bg-slate-800 p-4">
          <h2 class="mb-2 text-lg font-semibold text-white">Seasonal budgets</h2>
          <div class="grid gap-4 sm:grid-cols-2">
            <div v-if="store.summary.transfer_budget" class="text-sm">
              <span class="block text-slate-400">Transfer budget ({{ store.summary.transfer_budget.season }})</span>
              <span class="text-slate-200">{{ fmt(store.summary.transfer_budget.allocated) }} allocated · {{
                fmt(store.summary.transfer_budget.committed) }} committed</span>
              <span class="block text-emerald-300">{{ fmt(store.summary.transfer_budget.available) }} available</span>
            </div>
            <div v-if="store.summary.wage_budget" class="text-sm">
              <span class="block text-slate-400">Wage budget ({{ store.summary.wage_budget.season }})</span>
              <span class="text-slate-200">{{ fmt(store.summary.wage_budget.allocated) }} allocated · {{
                fmt(store.summary.wage_budget.committed) }} committed</span>
              <span class="block text-emerald-300">{{ fmt(store.summary.wage_budget.available) }} available</span>
            </div>
          </div>
        </section>

        <section class="rounded border border-slate-700 bg-slate-800 p-4">
          <h2 class="mb-2 text-lg font-semibold text-white">Wage commitments</h2>
          <p class="text-sm text-slate-300">
            {{ store.summary.wage_commitments.count }} active contracts ·
            {{ fmt(store.summary.wage_commitments.weekly_wage) }} weekly ·
            {{ fmt(store.summary.wage_commitments.annual_wage) }} annualised
          </p>
          <p class="mt-1 text-sm text-slate-400">
            Committed spending {{ fmt(store.summary.committed_spending) }} ·
            projected revenue {{ fmt(store.summary.projected_revenue) }} ·
            future installments {{ fmt(store.summary.future_installments) }} ·
            debt {{ fmt(store.summary.debt) }}
          </p>
        </section>

        <section v-if="store.summary.factors.length" class="rounded border border-slate-700 bg-slate-800 p-4">
          <h2 class="mb-2 text-lg font-semibold text-white">Where the cash comes from</h2>
          <ul class="space-y-1 text-sm">
            <li v-for="f in store.summary.factors" :key="f.label" class="flex justify-between">
              <span class="text-slate-300">{{ f.label }}</span>
              <span :class="f.amount < 0 ? 'text-red-300' : 'text-emerald-300'">{{ sign(f.amount) }}</span>
            </li>
            <li class="flex justify-between border-t border-slate-700 pt-1 font-medium">
              <span>Total</span><span class="text-emerald-300">{{ fmt(factSum()) }}</span>
            </li>
          </ul>
        </section>

        <section class="rounded border border-slate-700 bg-slate-800 p-4">
          <h2 class="mb-2 text-lg font-semibold text-white">Recent ledger activity</h2>
          <table class="w-full text-sm">
            <thead>
              <tr class="text-left text-slate-400">
                <th class="pb-1">Date</th>
                <th>Type</th>
                <th>Category</th>
                <th class="text-right">Amount</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="e in store.ledger.slice(0, 20)" :key="e.id" class="border-t border-slate-700">
                <td class="py-1">{{ new Date(e.occurred_at).toLocaleDateString() }}</td>
                <td>{{ e.entry_type }}</td>
                <td class="text-slate-300">{{ e.description || e.category }}</td>
                <td class="text-right" :class="e.entry_type === 'credit' ? 'text-emerald-300' : 'text-red-300'">
                  {{ e.entry_type === 'credit' ? '+' : '−' }}{{ fmt(e.amount) }}
                </td>
              </tr>
            </tbody>
          </table>
        </section>

        <section class="rounded border border-slate-700 bg-slate-800 p-4">
          <h2 class="mb-2 text-lg font-semibold text-white">Contracts</h2>
          <table class="w-full text-sm">
            <thead>
              <tr class="text-left text-slate-400">
                <th class="pb-1">Player</th>
                <th>Weekly wage</th>
                <th>Start</th>
                <th>Until</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="c in store.contracts" :key="c.id" class="border-t border-slate-700">
                <td class="py-1">{{ c.player.name }}</td>
                <td>{{ fmt(c.weekly_wage) }}</td>
                <td>{{ c.start_date }}</td>
                <td>{{ c.end_date }}</td>
                <td>{{ c.status }}</td>
              </tr>
            </tbody>
          </table>
        </section>
      </template>
    </div>
  </main>
</template>