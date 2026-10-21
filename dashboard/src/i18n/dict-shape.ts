// 字典结构工具：en 命名空间是键结构真源，其他语言用 Shape<typeof en> 声明，
// 缺键/多键/非字符串叶子都会在编译期报错。

export type DictNode = { readonly [key: string]: string | DictNode };

// 生成与 T 同构的类型：键集合完全相同，叶子统一放宽为 string。
export type Shape<T extends DictNode> = {
  readonly [K in keyof T]: T[K] extends string
    ? string
    : Shape<Extract<T[K], DictNode>>;
};

// 生成 "a.b.c" 形式的全部合法键路径，用作 t() 的严格键类型。
export type Paths<T> = T extends string
  ? never
  : {
      [K in keyof T & string]: T[K] extends string ? K : `${K}.${Paths<T[K]>}`;
    }[keyof T & string];
