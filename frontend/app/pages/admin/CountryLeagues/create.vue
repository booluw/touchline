<script lang="ts" setup>
import z from 'zod';
import type { Country } from '~/types';

const route = useRoute()
const router = useRouter()
const store = useAdminStore()
const { createLeague } = useAdmin()

const countryId = computed(() => String(route.params.countryId))

const country = computed(() => store.countries?.find((c: Country) => c.id === countryId.value))
const leagues = computed(() => store.leagues?.filter((l: League) => l.country.id === countryId.value) ?? [])

const schema = z.object({
  name: z.string().min(3, 'league name must be more than 3 letters'),
  team_count: z.number().min(5, 'league requires at least 5 teams'),
  promotes_to: z.string().nullable(),
  promotions: z.number(),
  relegates_to: z.string().nullable(),
  relegations: z.number(),
  tier: z.number(),
})
const loading = ref(false)
const state = ref({
  name: '',
  team_count: 0,
  promotes_to: null,
  promotions: 0,
  relegates_to: null,
  relegations: 0,
  tier: 1,
  country_id: countryId.value
})

async function createNewLeague(valid: boolean, errors: Record<string, string>) {
  console.log(valid, errors)
  if (valid) {
    loading.value = true
    try {
      await createLeague(state.value)
      router.go(-1)
    } finally {
      loading.value = false
    }
  }
}
</script>

<template>
  <OldUiSlide @close="() => router.go(-1)" title="Create League" :description="`Create a new league for ${country?.name ?? ''}`">
    <OldUiForm @submit="createNewLeague" class="h-full flex flex-col gap-10" :state :schema>
      <div class="">
        <div class="grid gap-3 md:grid-cols-5">
          <OldUiFormItem class="col-span-3" label="Name" prop="name">
            <OldUiInput v-model="state.name" :placeholder="`The ${country?.name ?? ''} Premier League`" />
          </OldUiFormItem>
          <OldUiFormItem label="Tier" prop="tier">
            <OldUiInput v-model="state.tier" type="number" placeholder="League Tier" />
          </OldUiFormItem>
          <OldUiFormItem label="Teams" prop="team_count">
            <OldUiInput v-model="state.team_count" type="number" placeholder="Number of teams" />
          </OldUiFormItem>
        </div>

        <div class="">
          <h3 class="mb-2 font-mono uppercase text-sm font-semibold">Promotions</h3>
          <div class="grid md:grid-cols-2 gap-3">
            <OldUiFormItem label="Promotion league" prop="promotes_to">
              <OldUiSelect v-model="state.promotes_to" :options="leagues" item-id="id" item-val="name" placeholder="Select League For Promotion" />
            </OldUiFormItem>
            <OldUiFormItem label="Teams To Promote" prop="promotions">
              <OldUiInput v-model="state.promotions" type="number" placeholder="Number of teams to promote" />
            </OldUiFormItem>
          </div>
        </div>

        <div class="">
          <h3 class="mb-2 font-mono uppercase text-sm font-semibold">Relegations</h3>
          <div class="grid md:grid-cols-2 gap-3">
            <OldUiFormItem label="Relegation league" prop="relegates_to">
              <OldUiSelect v-model="state.relegates_to" :options="leagues" item-id="id" item-val="name"
                placeholder="Select League For Relegation" />
            </OldUiFormItem>
            <OldUiFormItem label="Teams To Relegate" prop="relegations">
              <OldUiInput v-model="state.relegations" type="number" placeholder="Number of teams to relegate" />
            </OldUiFormItem>
          </div>
        </div>
      </div>

      <button type="submit" class="button button--primary">
        Create {{ country?.name }} League
      </button>
    </OldUiForm>
  </OldUiSlide>
</template>