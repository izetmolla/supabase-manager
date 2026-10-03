import { useState } from 'react'
import { Check, Copy, Eye, EyeOff } from 'lucide-react'
import { toast } from 'sonner'
import { Button } from '@workspace/ui/components/button'
import { Input } from '@workspace/ui/components/input'
import { Label } from '@workspace/ui/components/label'

interface CopyFieldProps {
  label?: string
  value: string
  secret?: boolean
  description?: string
}

export function CopyField({ label, value, secret, description }: CopyFieldProps) {
  const [revealed, setRevealed] = useState(!secret)
  const [copied, setCopied] = useState(false)

  const copy = async () => {
    await navigator.clipboard.writeText(value)
    setCopied(true)
    toast.success(`${label ?? 'Value'} copied`)
    setTimeout(() => setCopied(false), 1500)
  }

  return (
    <div className="grid gap-1.5">
      {label && <Label className="text-muted-foreground text-xs">{label}</Label>}
      <div className="flex gap-1.5">
        <Input
          readOnly
          value={revealed ? value : '•'.repeat(Math.min(value.length, 40))}
          className="font-mono text-xs"
          onFocus={(e) => e.currentTarget.select()}
        />
        {secret && (
          <Button variant="outline" size="icon" onClick={() => setRevealed((r) => !r)} aria-label="Toggle visibility">
            {revealed ? <EyeOff /> : <Eye />}
          </Button>
        )}
        <Button variant="outline" size="icon" onClick={copy} aria-label="Copy" disabled={!value}>
          {copied ? <Check className="text-brand" /> : <Copy />}
        </Button>
      </div>
      {description && <p className="text-muted-foreground text-xs">{description}</p>}
    </div>
  )
}
