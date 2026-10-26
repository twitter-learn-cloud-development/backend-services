<script setup lang="ts">
import { computed } from 'vue'

type InlinePart = { text: string; kind: 'text' | 'strong' | 'code' | 'link'; href?: string }
type ContentBlock = {
  kind: 'paragraph' | 'heading' | 'list' | 'quote' | 'code'
  text?: string
  items?: string[]
  ordered?: boolean
  level?: number
}

const props = defineProps<{ content: string }>()

const parseBlocks = (content: string): ContentBlock[] => {
  const lines = content.replace(/\r\n?/g, '\n').split('\n')
  const blocks: ContentBlock[] = []
  let index = 0
  while (index < lines.length) {
    const line = lines[index]!
    if (!line.trim()) {
      index += 1
      continue
    }
    if (line.trimStart().startsWith('```')) {
      index += 1
      const code: string[] = []
      while (index < lines.length && !lines[index]!.trimStart().startsWith('```')) {
        code.push(lines[index]!)
        index += 1
      }
      if (index < lines.length) index += 1
      blocks.push({ kind: 'code', text: code.join('\n') })
      continue
    }
    const heading = /^(#{1,3})\s+(.+)$/.exec(line)
    if (heading) {
      blocks.push({ kind: 'heading', level: heading[1]!.length, text: heading[2]!.trim() })
      index += 1
      continue
    }
    const list = /^(\s*)([-*]|\d+\.)\s+(.+)$/.exec(line)
    if (list) {
      const ordered = /\d/.test(list[2]!)
      const items: string[] = []
      while (index < lines.length) {
        const item = /^(\s*)([-*]|\d+\.)\s+(.+)$/.exec(lines[index]!)
        if (!item || /\d/.test(item[2]!) !== ordered) break
        items.push(item[3]!.trim())
        index += 1
      }
      blocks.push({ kind: 'list', items, ordered })
      continue
    }
    if (/^>\s?/.test(line)) {
      const quoted: string[] = []
      while (index < lines.length && /^>\s?/.test(lines[index]!)) {
        quoted.push(lines[index]!.replace(/^>\s?/, ''))
        index += 1
      }
      blocks.push({ kind: 'quote', text: quoted.join('\n') })
      continue
    }
    const paragraph: string[] = []
    while (index < lines.length && lines[index]!.trim()
      && !/^(#{1,3}\s+|\s*[-*]\s+|\s*\d+\.\s+|>\s?|\s*```)/.test(lines[index]!)) {
      paragraph.push(lines[index]!.trim())
      index += 1
    }
    blocks.push({ kind: 'paragraph', text: paragraph.join('\n') })
  }
  return blocks
}

const inlineParts = (text: string): InlinePart[] => {
  const parts: InlinePart[] = []
  const pattern = /(\*\*[^*\n]+\*\*|`[^`\n]+`|\[[^\]\n]+\]\(https?:\/\/[^\s)]+\))/g
  let cursor = 0
  for (const match of text.matchAll(pattern)) {
    const position = match.index ?? cursor
    if (position > cursor) parts.push({ kind: 'text', text: text.slice(cursor, position) })
    const token = match[0]
    if (token.startsWith('**')) {
      parts.push({ kind: 'strong', text: token.slice(2, -2) })
    } else if (token.startsWith('`')) {
      parts.push({ kind: 'code', text: token.slice(1, -1) })
    } else {
      const separator = token.lastIndexOf('](')
      parts.push({ kind: 'link', text: token.slice(1, separator), href: token.slice(separator + 2, -1) })
    }
    cursor = position + token.length
  }
  if (cursor < text.length) parts.push({ kind: 'text', text: text.slice(cursor) })
  return parts
}

const blocks = computed(() => parseBlocks(props.content))
</script>

<template>
  <div class="space-y-3 break-words text-sm leading-6 text-gray-900 dark:text-gray-100">
    <template v-for="(block, blockIndex) in blocks" :key="blockIndex">
      <h3 v-if="block.kind === 'heading'" class="font-semibold" :class="block.level === 1 ? 'text-base' : 'text-sm'">
        {{ block.text }}
      </h3>
      <pre v-else-if="block.kind === 'code'" class="overflow-x-auto rounded bg-gray-200 p-3 text-xs leading-5 dark:bg-gray-950"><code>{{ block.text }}</code></pre>
      <blockquote v-else-if="block.kind === 'quote'" class="whitespace-pre-line border-l-2 border-gray-400 pl-3 text-gray-600 dark:text-gray-300">{{ block.text }}</blockquote>
      <component :is="block.ordered ? 'ol' : 'ul'" v-else-if="block.kind === 'list'" class="space-y-1 pl-5" :class="block.ordered ? 'list-decimal' : 'list-disc'">
        <li v-for="(item, itemIndex) in block.items" :key="itemIndex" class="whitespace-pre-line">
          <template v-for="(part, partIndex) in inlineParts(item)" :key="partIndex">
            <strong v-if="part.kind === 'strong'">{{ part.text }}</strong>
            <code v-else-if="part.kind === 'code'" class="rounded bg-gray-200 px-1 dark:bg-gray-700">{{ part.text }}</code>
            <a v-else-if="part.kind === 'link'" :href="part.href" target="_blank" rel="noopener noreferrer" class="text-primary underline">{{ part.text }}</a>
            <span v-else>{{ part.text }}</span>
          </template>
        </li>
      </component>
      <p v-else class="whitespace-pre-line">
        <template v-for="(part, partIndex) in inlineParts(block.text || '')" :key="partIndex">
          <strong v-if="part.kind === 'strong'">{{ part.text }}</strong>
          <code v-else-if="part.kind === 'code'" class="rounded bg-gray-200 px-1 dark:bg-gray-700">{{ part.text }}</code>
          <a v-else-if="part.kind === 'link'" :href="part.href" target="_blank" rel="noopener noreferrer" class="text-primary underline">{{ part.text }}</a>
          <span v-else>{{ part.text }}</span>
        </template>
      </p>
    </template>
  </div>
</template>
