import type { EQPreset } from '~~/shared/types/audio'

export const BUILTIN_PRESETS: EQPreset[] = [
  { name: 'Flat', builtin: true, preamp: 0, postgain: 0, bands: [0, 0, 0, 0, 0, 0, 0, 0, 0, 0] },
  { name: 'Bass Boost', builtin: true, preamp: -2, postgain: 0, bands: [6, 5, 4, 2, 1, 0, 0, 0, 0, 0] },
  { name: 'Vocal', builtin: true, preamp: -1, postgain: 0, bands: [-2, -1, 0, 2, 4, 4, 3, 1, 0, -1] },
  { name: 'Treble Boost', builtin: true, preamp: -2, postgain: 0, bands: [0, 0, 0, 0, 0, 1, 2, 4, 5, 6] },
  { name: 'Rock', builtin: true, preamp: -1, postgain: 0, bands: [4, 3, 1, -1, -2, -1, 1, 3, 4, 5] },
  { name: 'Electronic', builtin: true, preamp: -2, postgain: 0, bands: [5, 4, 2, 0, -2, -1, 1, 3, 4, 5] },
  { name: 'Acoustic', builtin: true, preamp: -1, postgain: 0, bands: [3, 2, 1, 1, 0, 1, 2, 2, 3, 2] },
  { name: 'Classical', builtin: true, preamp: 0, postgain: 0, bands: [3, 2, 1, 0, 0, 0, 0, 1, 2, 3] },
  { name: 'Hip-Hop', builtin: true, preamp: -2, postgain: 0, bands: [5, 4, 3, 1, 0, -1, 0, 1, 2, 3] },
  { name: 'Jazz', builtin: true, preamp: -1, postgain: 0, bands: [2, 1, 0, 1, 2, 2, 1, 1, 2, 3] },
  { name: 'R&B', builtin: true, preamp: -2, postgain: 0, bands: [4, 3, 1, 0, -1, 0, 1, 2, 3, 3] },
  { name: 'Podcast/Speech', builtin: true, preamp: 0, postgain: 0, bands: [-3, -2, 0, 3, 5, 5, 3, 1, -1, -3] },
]
