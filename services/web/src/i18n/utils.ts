// i18n utility functions — translation lookup, locale extraction, path building.

import { defaultLocale, isLocale, type Locale } from './config';
import en, { type TranslationKey } from './en';
import es from './es';

const translations: Record<Locale, Record<string, string>> = { en, es };

/**
 * Get a translated string for the given locale and key.
 * Supports {placeholder} interpolation via the params argument.
 * Falls back to English, then to the raw key.
 */
export function t(locale: Locale, key: TranslationKey, params?: Record<string, string>): string {
  let value = translations[locale]?.[key] ?? translations[defaultLocale]?.[key] ?? key;
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      value = value.replaceAll(`{${k}}`, v);
    }
  }
  return value;
}

/** Extract locale from a URL pathname like /en/about → 'en'. */
export function getLocaleFromUrl(url: URL): Locale {
  const segment = url.pathname.split('/')[1];
  return isLocale(segment ?? '') ? segment as Locale : defaultLocale;
}

/** Build a locale-prefixed path. localePath('es', '/about') → '/es/about'. */
export function localePath(locale: Locale, path: string): string {
  const clean = path.startsWith('/') ? path : `/${path}`;
  return `/${locale}${clean}`;
}

/**
 * Switch the locale of a URL path.
 * switchLocalePath('/en/about', 'es') → '/es/about'
 */
export function switchLocalePath(pathname: string, newLocale: Locale): string {
  const segments = pathname.split('/').filter(Boolean);
  if (segments.length > 0 && isLocale(segments[0])) {
    segments[0] = newLocale;
  } else {
    segments.unshift(newLocale);
  }
  return '/' + segments.join('/');
}

/**
 * Get all translation strings for the MagicBar (Preact island).
 * Returns a plain object that can be serialized as a prop.
 */
export function getMagicBarTranslations(locale: Locale) {
  return {
    navItems: [
      { titleKey: 'nav.home' as TranslationKey, descKey: 'magicbar.desc.home' as TranslationKey, target: localePath(locale, '/'), icon: 'fa-house' },
      { titleKey: 'nav.about' as TranslationKey, descKey: 'magicbar.desc.about' as TranslationKey, target: localePath(locale, '/about'), icon: 'fa-user' },
      { titleKey: 'nav.login' as TranslationKey, descKey: 'magicbar.desc.login' as TranslationKey, target: localePath(locale, '/login'), icon: 'fa-right-to-bracket' },
      { titleKey: 'nav.signup' as TranslationKey, descKey: 'magicbar.desc.signup' as TranslationKey, target: localePath(locale, '/signup'), icon: 'fa-user-plus' },
      { titleKey: 'nav.dashboard' as TranslationKey, descKey: 'magicbar.desc.dashboard' as TranslationKey, target: localePath(locale, '/dashboard'), icon: 'fa-gauge' },
      { titleKey: 'nav.logout' as TranslationKey, descKey: 'magicbar.desc.logout' as TranslationKey, target: '/logout', icon: 'fa-right-from-bracket' },
      { titleKey: 'nav.lies' as TranslationKey, descKey: 'magicbar.desc.lies' as TranslationKey, target: localePath(locale, '/lies'), icon: 'fa-mask' },
      { titleKey: 'nav.contact' as TranslationKey, descKey: 'magicbar.desc.contact' as TranslationKey, target: 'mailto:dev@jredh.com', icon: 'fa-envelope' },
    ].map(item => ({
      title: t(locale, item.titleKey),
      description: t(locale, item.descKey),
      type: 'navigation' as const,
      target: item.target,
      icon: item.icon,
    })),
    sectionNavigate: t(locale, 'magicbar.section.navigate'),
    sectionSearch: t(locale, 'magicbar.section.search'),
    searching: t(locale, 'magicbar.searching'),
    noResults: t(locale, 'magicbar.noResults'),
    ariaLabel: t(locale, 'magicbar.ariaLabel'),
    placeholderHero: t(locale, 'magicbar.placeholder.hero'),
    placeholderCompact: t(locale, 'magicbar.placeholder.compact'),
  };
}
