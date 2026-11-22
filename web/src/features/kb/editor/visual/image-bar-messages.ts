// imageBarMessages are the words of the bar that shows over a picked image in the visual editor.
export const imageBarMessages = {
  label: 'Image',
  width: 'Width',
  widths: { original: 'Original size', small: 'Small', medium: 'Medium', full: 'Full width', pixels: 'Pixels…' },
  pixels: 'Width in pixels',
  pixelsInvalid: (max: number) => `A width from 1 to ${max.toLocaleString('en')} pixels.`,
  alignment: 'Alignment',
  aligns: { default: 'Default', left: 'Left, text wraps', center: 'Centre', right: 'Right, text wraps', inline: 'In the line' },
  border: 'Border',
  caption: 'Caption',
  link: 'Link',
  annotate: 'Annotate',
};
