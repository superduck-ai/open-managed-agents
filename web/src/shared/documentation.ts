const documentationOrigin = 'https://oma.mintlifysite.com/docs';

export function documentationHref(path: string, locale: string) {
  return `${documentationOrigin}/${locale === 'zh-CN' ? 'zh' : 'en'}/${path}`;
}
