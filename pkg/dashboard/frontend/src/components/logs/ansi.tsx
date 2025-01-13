export const ansiToReact = (msg: string): React.ReactNode[] => {
  // Map ANSI codes to Tailwind CSS classes, this only supports the most common foreground codes
  // https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_(Select_Graphic_Rendition)_parameters
  const ansiClassMap: Record<string, string> = {
    // Foreground colors
    '30': 'text-black',
    '31': 'text-red-600',
    '32': 'text-green-600',
    '33': 'text-yellow-600',
    '34': 'text-blue-600',
    '35': 'text-purple-600',
    '36': 'text-cyan-600',
    '37': 'text-gray-600',
    // Reset
    '39': 'text-inherit',
  }

  const parts: React.ReactNode[] = []
  const regex = new RegExp(`${String.fromCharCode(27)}\\[([0-9]+)m`, 'g')
  let lastIndex = 0
  let currentClass = ''

  msg.replace(regex, (match, code, offset) => {
    if (offset > lastIndex) {
      parts.push(
        <span key={lastIndex} className={currentClass}>
          {msg.slice(lastIndex, offset)}
        </span>,
      )
    }

    currentClass = ansiClassMap[code] || ''
    lastIndex = offset + match.length

    return '' // Required for `.replace` but unused
  })

  // Add any remaining text
  if (lastIndex < msg.length) {
    parts.push(
      <span key={lastIndex} className={currentClass}>
        {msg.slice(lastIndex)}
      </span>,
    )
  }

  return parts
}
