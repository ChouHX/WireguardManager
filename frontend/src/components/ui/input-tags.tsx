"use client"

import * as React from "react"
import { X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"

export interface InputTagsProps {
  value: string[]
  onChange: (tags: string[]) => void
  placeholder?: string
  validate?: (tag: string) => boolean
  normalize?: (tag: string) => string
  errorMessage?: string
  className?: string
  disabled?: boolean
}

export function InputTags({
  value,
  onChange,
  placeholder,
  validate,
  normalize,
  errorMessage,
  className,
  disabled = false,
}: InputTagsProps) {
  const [inputValue, setInputValue] = React.useState("")
  const [error, setError] = React.useState("")
  const inputRef = React.useRef<HTMLInputElement>(null)

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === "Enter" || e.key === "," || e.key === " ") {
      e.preventDefault()
      addTag()
    } else if (e.key === "Backspace" && !inputValue && value.length > 0) {
      removeTag(value.length - 1)
    }
  }

  const addTag = () => {
    const trimmedValue = inputValue.trim()
    if (!trimmedValue) return

    // 验证标签
    if (validate && !validate(trimmedValue)) {
      setError(errorMessage || "Invalid input")
      return
    }

    // 规范化标签（例如：IP 地址自动添加 CIDR 后缀）
    const normalizedValue = normalize ? normalize(trimmedValue) : trimmedValue

    // 检查是否已存在
    if (value.includes(normalizedValue)) {
      setError("Already exists")
      return
    }

    onChange([...value, normalizedValue])
    setInputValue("")
    setError("")
  }

  const removeTag = (index: number) => {
    onChange(value.filter((_, i) => i !== index))
  }

  const handleBlur = () => {
    addTag()
  }

  return (
    <div className={cn("space-y-2", className)}>
      <div
        className={cn(
          "flex min-h-10 w-full flex-wrap gap-2 rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background focus-within:ring-2 focus-within:ring-ring focus-within:ring-offset-2",
          disabled && "cursor-not-allowed opacity-50"
        )}
        onClick={() => inputRef.current?.focus()}
      >
        {value.map((tag, index) => (
          <Badge
            key={index}
            variant="secondary"
            className="gap-1 pr-1"
          >
            <span>{tag}</span>
            {!disabled && (
              <button
                type="button"
                className="ml-1 rounded-full outline-none ring-offset-background focus:ring-2 focus:ring-ring focus:ring-offset-2"
                onClick={(e) => {
                  e.stopPropagation()
                  removeTag(index)
                }}
              >
                <X className="h-3 w-3" />
              </button>
            )}
          </Badge>
        ))}
        <Input
          ref={inputRef}
          value={inputValue}
          onChange={(e) => {
            setInputValue(e.target.value)
            setError("")
          }}
          onKeyDown={handleKeyDown}
          onBlur={handleBlur}
          placeholder={value.length === 0 ? placeholder : ""}
          disabled={disabled}
          className="flex-1 border-0 bg-transparent p-0 placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-0 focus-visible:ring-offset-0 min-w-[120px]"
        />
      </div>
      {error && (
        <p className="text-sm text-red-600 dark:text-red-400">{error}</p>
      )}
    </div>
  )
}
