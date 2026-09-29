/** downloadText saves text as a file through a temporary object URL (the browser's download flow). */
export function downloadText(filename: string, text: string): void {
  const url = URL.createObjectURL(new Blob([text], { type: 'application/json' }));
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  a.click();
  // Revoke late: Firefox can still be starting the download when a 0 ms timer fires.
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
