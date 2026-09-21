import { nextTick, onBeforeUnmount, watch, type Ref } from 'vue'

const focusableSelector = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  '[contenteditable="true"]',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

function focusableElements(dialog: HTMLElement): HTMLElement[] {
  return Array.from(dialog.querySelectorAll<HTMLElement>(focusableSelector)).filter((element) => !element.hasAttribute('disabled'))
}

export function useDialogFocus(
  open: Ref<boolean>,
  dialogRef: Ref<HTMLElement | null>,
  initialFocusRef: Ref<HTMLElement | null>,
  onClose: () => void,
): void {
  let previouslyFocused: HTMLElement | null = null
  let listening = false

  function removeListener(): void {
    if (!listening) return
    document.removeEventListener('keydown', handleKeydown)
    listening = false
  }

  function restoreFocus(): void {
    removeListener()
    const target = previouslyFocused
    previouslyFocused = null
    if (target?.isConnected) target.focus()
  }

  function handleKeydown(event: KeyboardEvent): void {
    if (!open.value) return
    const dialog = dialogRef.value
    if (!dialog) return

    if (event.key === 'Escape') {
      event.preventDefault()
      onClose()
      return
    }
    if (event.key !== 'Tab') return

    const elements = focusableElements(dialog)
    if (elements.length === 0) {
      event.preventDefault()
      return
    }

    const active = document.activeElement
    const currentIndex = active instanceof HTMLElement ? elements.indexOf(active) : -1
    const nextIndex = event.shiftKey
      ? currentIndex <= 0 ? elements.length - 1 : currentIndex - 1
      : currentIndex === elements.length - 1 ? 0 : currentIndex + 1

    event.preventDefault()
    elements[nextIndex].focus()
  }

  async function focusInitialElement(): Promise<void> {
    await nextTick()
    if (!open.value) return

    const initial = initialFocusRef.value
    if (initial?.isConnected && !initial.hasAttribute('disabled')) {
      initial.focus()
      return
    }
    focusableElements(dialogRef.value ?? document.createElement('div'))[0]?.focus()
  }

  watch(open, (isOpen) => {
    if (!isOpen) {
      restoreFocus()
      return
    }

    const active = document.activeElement
    previouslyFocused = active instanceof HTMLElement ? active : null
    if (!listening) {
      document.addEventListener('keydown', handleKeydown)
      listening = true
    }
    void focusInitialElement()
  }, { immediate: true })

  onBeforeUnmount(restoreFocus)
}
