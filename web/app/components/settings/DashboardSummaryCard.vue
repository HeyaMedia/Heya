<script setup lang="ts">
withDefaults(defineProps<{
  title: string
  icon: string
  value: string | number
  valueLabel?: string
  tone?: 'neutral' | 'good' | 'warn' | 'bad'
  alert?: string | number
  alertLabel?: string
}>(), {
  valueLabel: '',
  tone: 'neutral',
  alert: '',
  alertLabel: '',
})
</script>

<template>
  <section class="dashboard-summary" :class="`tone-${tone}`">
    <header class="dashboard-summary-head">
      <div class="dashboard-summary-title">
        <span class="dashboard-summary-icon"><Icon :name="icon" :size="15" /></span>
        <span>{{ title }}</span>
      </div>
      <div class="dashboard-summary-total">
        <span class="dashboard-summary-value">{{ value }}</span>
        <span v-if="valueLabel" class="dashboard-summary-value-label">{{ valueLabel }}</span>
        <span v-if="alert !== ''" class="dashboard-summary-alert">
          <strong>{{ alert }}</strong>
          <span v-if="alertLabel">{{ alertLabel }}</span>
        </span>
      </div>
    </header>

    <div class="dashboard-summary-body"><slot /></div>
    <div v-if="$slots.footer" class="dashboard-summary-footer"><slot name="footer" /></div>
  </section>
</template>

<style scoped>
.dashboard-summary {
  min-width: 0;
  min-height: 228px;
  display: flex;
  flex-direction: column;
  padding: 17px 18px 15px;
  border: 1px solid var(--hair);
  border-radius: var(--r-lg);
  background: linear-gradient(150deg, var(--bg-1), color-mix(in srgb, var(--bg-2) 76%, var(--bg-1)));
  box-shadow: var(--shadow-el);
  transition: box-shadow 0.28s ease;
}
.dashboard-summary.tone-good { border-color: color-mix(in srgb, var(--good) 22%, var(--border)); }
.dashboard-summary.tone-warn { border-color: color-mix(in srgb, var(--gold) 32%, var(--border)); }
.dashboard-summary.tone-bad { border-color: color-mix(in srgb, var(--bad) 36%, var(--border)); }

.dashboard-summary-head {
  min-height: 46px;
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  padding-bottom: 13px;
  border-bottom: 1px solid var(--hair);
}
/* Mono uppercase head (Heya 2.0 sec-head) with the brand-gold icon tile. */
.dashboard-summary-title {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  color: var(--fg-1);
  font-family: var(--font-mono);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.12em;
  text-transform: uppercase;
}
.dashboard-summary-icon {
  width: 27px;
  height: 27px;
  display: grid;
  place-items: center;
  flex-shrink: 0;
  border-radius: var(--r-sm);
  background: var(--gold-soft);
  color: var(--gold);
}
.dashboard-summary-total {
  display: flex;
  align-items: baseline;
  justify-content: flex-end;
  gap: 4px;
  flex-wrap: wrap;
  text-align: right;
}
.dashboard-summary-value {
  color: var(--fg-0);
  font-size: 24px;
  font-weight: 680;
  letter-spacing: -0.04em;
  line-height: 1;
  font-variant-numeric: tabular-nums;
}
.dashboard-summary-value-label {
  color: var(--fg-3);
  font-size: 10px;
  font-weight: 550;
  text-transform: uppercase;
  letter-spacing: 0.06em;
}
.dashboard-summary-alert {
  width: fit-content;
  min-height: 21px;
  flex-basis: 100%;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  margin: 2px 0 0 auto;
  padding: 2px 7px;
  border: 1px solid color-mix(in srgb, var(--bad) 24%, transparent);
  border-radius: 999px;
  background: color-mix(in srgb, var(--bad) 8%, transparent);
  color: var(--bad);
  font-family: var(--font-mono);
  font-size: 11px;
  font-weight: 570;
  line-height: 1;
  white-space: nowrap;
}
.dashboard-summary-alert strong { font-size: 12px; font-weight: 720; }

.dashboard-summary-body {
  display: flex;
  flex-direction: column;
  gap: 1px;
  padding-top: 10px;
}
.dashboard-summary-body :deep(.summary-row) {
  min-height: 27px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  color: var(--fg-3);
  font-size: 11.5px;
}
.dashboard-summary-body :deep(.summary-row strong) {
  color: var(--fg-1);
  font-family: var(--font-mono);
  font-size: 12px;
  font-weight: 620;
  font-variant-numeric: tabular-nums;
}
.dashboard-summary-body :deep(.summary-row strong.good) { color: var(--good); }
.dashboard-summary-body :deep(.summary-row strong.warn) { color: var(--gold); }
.dashboard-summary-body :deep(.summary-row strong.bad) { color: var(--bad); }

.dashboard-summary-footer {
  margin-top: auto;
  padding-top: 11px;
  border-top: 1px solid var(--hair);
}

@media (max-width: 620px) {
  .dashboard-summary { min-height: 0; }
}
</style>
