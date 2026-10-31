export const apiError = (
  status: number,
  code: string,
  message: string
): Response => Response.json({ error: { code, message } }, { status });
