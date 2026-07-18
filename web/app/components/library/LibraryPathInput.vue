<template>
  <div class="lpi">
    <PathBrowser
      :model-value="modelValue"
      @update:model-value="$emit('update:modelValue', $event)"
    />
    <div class="lpi-hint" :class="{ warning: isURLPath }">
      <Icon :name="isURLPath ? 'warning' : 'hard-drives'" :size="11" />
      <span v-if="isURLPath">
        URL library paths are no longer supported. Mount the share on the Heya host or container, then replace this with its filesystem path.
      </span>
      <span v-else>
        Network storage works through an OS or container mount; choose the mounted folder here.
      </span>
    </div>
  </div>
</template>

<script setup lang="ts">
const props = defineProps<{
  modelValue: string
}>()

defineEmits<{
  'update:modelValue': [value: string]
}>()

const isURLPath = computed(() => /^[a-z][a-z0-9+.-]*:\/\//i.test(props.modelValue.trim()))
</script>

<style scoped>
.lpi {
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.lpi-hint {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  padding: 0 2px;
  color: var(--fg-3);
  font-size: 10.5px;
  line-height: 1.4;
}

.lpi-hint :deep(svg) {
  flex: none;
  margin-top: 2px;
}

.lpi-hint.warning {
  color: var(--warning, #d9a441);
}
</style>
