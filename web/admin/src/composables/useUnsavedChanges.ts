import { ref } from 'vue'

const dirty = ref(false)
let allowNextLeave = false

export function setUnsavedChanges(value: boolean): void {
  dirty.value = value
}

export function hasUnsavedChanges(): boolean {
  return dirty.value
}

export function confirmLeave(): boolean {
  if (!dirty.value) return true
  return window.confirm('有未保存的修改，确定要离开吗？')
}

export function prepareNextLeave(): void {
  allowNextLeave = true
}

export function cancelPreparedLeave(): void {
  allowNextLeave = false
}

export function consumePreparedLeave(): boolean {
  if (!allowNextLeave) return false
  allowNextLeave = false
  return true
}
