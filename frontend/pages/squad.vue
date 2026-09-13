<script setup lang="ts">
import { useSquadStore } from '~/stores/squad'
const squad=useSquadStore(); const clubId=ref(''); const error=ref('')
async function load(){const {authedFetch}=useAuth();const r=await authedFetch('/api/clubs');const clubs=await r.json();clubId.value=clubs[0]?.id??'';if(!clubId.value)return;await squad.fetchSquad(clubId.value)}
onMounted(()=>load().catch(()=>error.value='Could not load your squad.'))
</script>
<template><main class="min-h-screen bg-slate-900 px-6 py-10 text-slate-200"><div class="mx-auto max-w-4xl space-y-6"><h1 class="text-3xl font-bold text-white">Squad</h1><p class="text-slate-400">Your available players. Select an eleven from the squad management API before matchday.</p><p v-if="error" class="text-red-400">{{error}}</p><div class="overflow-hidden rounded border border-slate-700 bg-slate-800"><div v-for="(p,i) in squad.players" :key="String(p.id ?? i)" class="flex border-b border-slate-700 px-4 py-3 text-sm last:border-0"><span class="w-10 text-slate-500">{{i+1}}</span><span class="flex-1 font-medium text-white">{{p.name ?? p.full_name ?? p.id}}</span><span class="text-slate-400">{{p.primary_position ?? p.position ?? ''}}</span></div><p v-if="!squad.players.length" class="p-4 text-slate-500">No squad is available yet.</p></div></div></main></template>
