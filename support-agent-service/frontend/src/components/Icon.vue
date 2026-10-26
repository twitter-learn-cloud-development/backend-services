<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 20 });
const paths: Record<string, string[]> = {
  plus: ["M12 5v14", "M5 12h14"],
  chat: [
    "M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-1 1v-9.5a8.5 8.5 0 1 1 18 0Z",
    "M7 10h10",
    "M7 14h6",
  ],
  arrow: ["M7 17 17 7", "M7 7h10v10"],
  up: ["M12 19V5", "m5 12 7-7 7 7"],
  chevron: ["m9 5 7 7-7 7"],
  down: ["m6 9 6 6 6-6"],
  layers: ["m12 3 9 5-9 5-9-5 9-5Z", "m3 12 9 5 9-5", "m3 16 9 5 9-5"],
  eye: [
    "M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12Z",
    "M15 12a3 3 0 1 1-6 0 3 3 0 0 1 6 0Z",
  ],
  sparkles: [
    "m12 3 2.2 6.8L21 12l-6.8 2.2L12 21l-2.2-6.8L3 12l6.8-2.2L12 3Z",
    "M20 2v4",
    "M18 4h4",
  ],
  check: ["m5 12 4 4L19 6"],
  shield: ["m12 3 8 3v6c0 5-8 9-8 9s-8-4-8-9V6l8-3Z", "m8 12 3 3 5-6"],
  search: ["M18 10a7 7 0 1 1-14 0 7 7 0 0 1 14 0Z", "m15 15 6 6"],
  settings: [
    "M12 8a4 4 0 1 1 0 8 4 4 0 0 1 0-8Z",
    "m10 3 4 0 1 3 3 1 3 3-2 3v2l2 3-3 3-3-1-2 1-1 3h-4l-1-3-3-1-3-3 2-3v-2l-2-3 3-3 3 1 2-1 1-3Z",
  ],
  book: [
    "M12 5v15",
    "M3 4h5c2 0 4 1 4 2 0-1 2-2 4-2h5v15h-5c-2 0-4 1-4 2-1-1-2-2-4-2H3V4Z",
  ],
  copy: ["M8 8h12v13H8V8Z", "M16 8V3H3v13h5"],
  close: ["m6 6 12 12", "M18 6 6 18"],
  menu: ["M4 6h16", "M4 12h16", "M4 18h16"],
  stop: ["M6 6h12v12H6z"],
  retry: ["M3 11a9 9 0 1 1 2 7", "M3 4v7h7"],
  link: [
    "m9 15 6-6",
    "M7 14 4 17a3 3 0 0 0 4 4l5-5a3 3 0 0 0 0-4",
    "m17 10 3-3a3 3 0 0 0-4-4l-5 5a3 3 0 0 0 0 4",
  ],
  logout: ["M9 3H3v18h6", "M9 12h12", "m17 8 4 4-4 4"],
  clock: ["M21 12a9 9 0 1 1-18 0 9 9 0 0 1 18 0Z", "M12 7v5l3 2"],
  dot: ["M12 4a8 8 0 1 1 0 16 8 8 0 0 1 0-16Z"],
};
</script>
<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.65"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
  >
    <path
      v-for="(path, index) in paths[name] || paths.chat"
      :key="index"
      :d="path"
    />
  </svg>
</template>
