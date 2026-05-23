// Ambient declarations for third-party modules that ship without their own
// TypeScript types.

declare module 'akarisub' {
  // The library exposes a default class. Type the shape loosely (its surface
  // is large and undocumented) so consumers can use it as both a value and a
  // type without us maintaining a parallel definition.
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  export default class AkariSub {
    constructor(...args: any[])
    [key: string]: any
  }
}
