<script setup lang="ts" generic="Row, K extends string">
import type { TableColumn, TableSort } from '~/types/ui/design'

/**
 * Names left, numbers right, status as pill. Sorting is controlled: the parent
 * owns `v-model:sort` and orders `rows` (server or client side).
 * Cells render `row[key]` by default; override with `#cell-<key>="{ row }"`.
 */
const props = withDefaults(defineProps<{
  columns: TableColumn<K>[]
  rows: Row[]
  rowKey: (row: Row) => string | number
  /** 'simple' hides columns marked `advanced`. */
  density?: 'simple' | 'standard'
  selectedKey?: string | number
  caption?: string
}>(), { density: 'standard' })
const sort = defineModel<TableSort<K> | undefined>('sort')
const emit = defineEmits<{ rowClick: [row: Row] }>()
defineSlots<{ [name: `cell-${string}`]: (props: { row: Row }) => unknown, empty?: () => unknown }>()

const visible = computed(() => props.columns.filter(c => props.density !== 'simple' || !c.advanced))
const template = computed(() => visible.value.map(c => c.width ?? '1fr').join(' '))

function toggleSort(column: TableColumn<K>) {
  if (!column.sortable) return
  const same = sort.value?.key === column.key
  sort.value = { key: column.key, direction: same && sort.value?.direction === 'desc' ? 'asc' : 'desc' }
}
function ariaSort(column: TableColumn<K>) {
  if (sort.value?.key !== column.key) return column.sortable ? 'none' : undefined
  return sort.value.direction === 'asc' ? 'ascending' : 'descending'
}
const cellValue = (row: Row, key: K) => (row as Record<string, unknown>)[key] as string | number | undefined
</script>

<template>
  <!-- Scrolls itself when its parent bounds the height; the header stays pinned. -->
  <div class="font-geist overflow-auto rounded-card border border-line bg-s1 text-[12.5px] tabular-nums">
    <table class="w-full" role="grid">
      <caption v-if="caption" class="sr-only">{{ caption }}</caption>
      <thead class="sticky top-0 z-[1] bg-s1">
        <tr class="grid gap-2 border-b border-line px-3 py-2" :style="{ gridTemplateColumns: template }">
          <th
            v-for="column in visible" :key="column.key" scope="col" :aria-sort="ariaSort(column)"
            :class="['text-label font-normal', column.align === 'right' ? 'text-right' : 'text-left', sort?.key === column.key ? 'text-t1' : 'text-t3']"
          >
            <button v-if="column.sortable" type="button" class="hover:text-t1" @click="toggleSort(column)">
              {{ column.label }}<span v-if="sort?.key === column.key"> {{ sort.direction === 'desc' ? '↓' : '↑' }}</span>
            </button>
            <template v-else>{{ column.label }}</template>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr
          v-for="row in rows" :key="rowKey(row)"
          :class="['grid cursor-pointer items-center gap-2 px-3 py-[9px] hover:bg-s2 [&+&]:border-t [&+&]:border-line', rowKey(row) === selectedKey && 'bg-s2']"
          :style="{ gridTemplateColumns: template }"
          :aria-selected="rowKey(row) === selectedKey"
          @click="emit('rowClick', row)"
        >
          <td v-for="column in visible" :key="column.key" :class="['min-w-0 text-t1', column.align === 'right' && 'text-right']">
            <slot :name="`cell-${column.key}`" :row="row">{{ cellValue(row, column.key) }}</slot>
          </td>
        </tr>
      </tbody>
    </table>
    <slot v-if="rows.length === 0" name="empty" />
  </div>
</template>
