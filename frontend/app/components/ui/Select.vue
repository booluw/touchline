<script setup lang="ts">
import {
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectItemIndicator,
  SelectItemText,
  SelectLabel,
  SelectPortal,
  SelectRoot,
  SelectScrollDownButton,
  SelectScrollUpButton,
  SelectSeparator,
  SelectTrigger,
  SelectValue,
  SelectViewport,
} from 'reka-ui'

const model = defineModel()
const props = withDefaults(defineProps<{
  options: string[] | { val: string, id: string }[] | Record<string, any>[]
  'item-id'?: string
  'item-val'?: string
  title?: string,
  class?: string
}>(), {
  'item-id': 'id',
  'item-val': 'val',
  title: 'Select option'
})

const errors = inject("f_errors") as Ref<Record<string, string>>
const f_prop = inject("f_prop")
const hasErrors = computed(() => errors ? Boolean(errors.value[f_prop as keyof typeof errors.value]) : false)

const inputClass = computed(() =>
  cn(
    "flex w-full mt-1 p-2 text-white font-mono border-brutal border-void-700 bg-void-800 placeholder:text-void-600 ring-transparent focus-visible:ring focus-visible:border-cyan-500 ease-brutal duration-80",
    props.class,
    hasErrors.value ? '!border-loss-500 focus-visible:ring-loss-500 !ring-transparent placeholder:!text-loss-500/50' : ''
  )
);
</script>

<template>
  <SelectRoot v-model="model">
    <SelectTrigger
      class="inline-flex min-w-10 items-center justify-between outline-none border-brutal"
      :class="inputClass"
      aria-label="Customise options"
    >
      <SelectValue :placeholder="$attrs.placeholder" />
      <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="#fff" viewBox="0 0 256 256">
        <path
          d="M213.66,101.66l-80,80a8,8,0,0,1-11.32,0l-80-80A8,8,0,0,1,53.66,90.34L128,164.69l74.34-74.35a8,8,0,0,1,11.32,11.32Z">
        </path>
      </svg>
    </SelectTrigger>

    <SelectPortal>
      <SelectContent
        class="min-w-10 border-brutal border-void-700 bg-void-600 will-change-[opacity,transform] data-[side=top]:animate-slideDownAndFade data-[side=right]:animate-slideLeftAndFade data-[side=bottom]:animate-slideUpAndFade data-[side=left]:animate-slideRightAndFade z-100"
        :side-offset="0">
        <SelectScrollUpButton class="flex items-center justify-center h-6.25 border-void-700 text-violet11 cursor-default">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="#fff" viewBox="0 0 256 256">
            <path
              d="M213.66,165.66a8,8,0,0,1-11.32,0L128,91.31,53.66,165.66a8,8,0,0,1-11.32-11.32l80-80a8,8,0,0,1,11.32,0l80,80A8,8,0,0,1,213.66,165.66Z">
            </path>
          </svg>
        </SelectScrollUpButton>

        <SelectViewport class="p-1.25">
          <SelectLabel class="px-6.25 text-xs leading-6.25 font-mono">
            {{ $attrs.placeholder }}
          </SelectLabel>
          <SelectGroup>
            <template v-for="(option, key) in options" :key>
              <SelectItem
                v-if="typeof option === 'string'"
                class="text-sm font-mono leading-none cursor-pointer flex items-center h-6.25 pr-8.75 pl-6.25 relative select-none data-disabled:text-mauve8 data-disabled:pointer-events-none data-highlighted:outline-none data-highlighted:bg-void-700 data-highlighted:text-void-300"
                :value="option">
                <SelectItemIndicator class="absolute left-0 w-6.25 inline-flex items-center justify-center">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="#000000" viewBox="0 0 256 256">
                    <path
                      d="M229.66,77.66l-128,128a8,8,0,0,1-11.32,0l-56-56a8,8,0,0,1,11.32-11.32L96,188.69,218.34,66.34a8,8,0,0,1,11.32,11.32Z">
                    </path>
                  </svg>
                </SelectItemIndicator>
                <SelectItemText>
                  {{ option }}
                </SelectItemText>
              </SelectItem>
              
              <SelectItem
                v-else
                class="text-xs leading-none text-grass11 rounded-[3px] flex items-center h-[25px] pr-[35px] pl-[25px] relative select-none data-[disabled]:text-mauve8 data-[disabled]:pointer-events-none data-[highlighted]:outline-none data-[highlighted]:bg-green9 data-[highlighted]:text-green1"
                :value="option[`${itemId}`]">
                <SelectItemIndicator class="absolute left-0 w-6.25 inline-flex items-center justify-center">
                  <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="#000000" viewBox="0 0 256 256">
                    <path
                      d="M229.66,77.66l-128,128a8,8,0,0,1-11.32,0l-56-56a8,8,0,0,1,11.32-11.32L96,188.69,218.34,66.34a8,8,0,0,1,11.32,11.32Z">
                    </path>
                  </svg>
                </SelectItemIndicator>
                <SelectItemText>
                  {{ option[`${itemVal}`] }}
                </SelectItemText>
              </SelectItem>
            </template>
          </SelectGroup>
        </SelectViewport>

        <SelectScrollDownButton class="flex items-center justify-center h-6.25 bg-white text-violet11 cursor-default">
          <svg xmlns="http://www.w3.org/2000/svg" class="h-3.5 w-3.5" fill="#000000" viewBox="0 0 256 256">
            <path
              d="M213.66,101.66l-80,80a8,8,0,0,1-11.32,0l-80-80A8,8,0,0,1,53.66,90.34L128,164.69l74.34-74.35a8,8,0,0,1,11.32,11.32Z">
            </path>
          </svg>
        </SelectScrollDownButton>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>