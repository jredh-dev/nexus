// Spanish translations.

import type { TranslationKey } from './en';

const es: Record<TranslationKey, string> = {
  // Brand
  'brand': 'Hooper Development',

  // Common
  'nav.home': 'Inicio',
  'nav.about': 'Acerca de',
  'nav.login': 'Iniciar sesión',
  'nav.signup': 'Registrarse',
  'nav.dashboard': 'Panel',
  'nav.logout': 'Cerrar sesión',
  'nav.contact': 'Contacto',
  'nav.lies': 'Mentiras',

  // MagicBar
  'magicbar.placeholder.hero': '¿A dónde quieres ir?',
  'magicbar.placeholder.compact': 'Buscar...',
  'magicbar.section.navigate': 'Navegar',
  'magicbar.section.search': 'Buscar',
  'magicbar.searching': 'Buscando...',
  'magicbar.noResults': 'Sin resultados',
  'magicbar.ariaLabel': 'Navegar o buscar',

  // MagicBar nav item descriptions
  'magicbar.desc.home': 'Ir a la página principal',
  'magicbar.desc.about': 'Conóceme',
  'magicbar.desc.login': 'Iniciar sesión en tu cuenta',
  'magicbar.desc.signup': 'Crear una cuenta nueva',
  'magicbar.desc.dashboard': 'Ver tu panel',
  'magicbar.desc.logout': 'Cerrar sesión',
  'magicbar.desc.contact': 'Envíame un correo',
  'magicbar.desc.lies': 'Ver secretos expuestos',

  // Homepage
  'home.brand': 'Hooper Development',

  // About page
  'about.title': 'Acerca de',
  'about.name': 'Jared Hooper',
  'about.jobTitle': 'Ingeniero de Software en Outreach',
  'about.bio1': 'Ingeniero de software en Seattle, WA. Trabajo en <a href="https://www.outreach.io" target="_blank" rel="noopener" style="color: var(--fresh-primary);">Outreach</a> y dedico mi tiempo libre a construir herramientas, agentes e infraestructura en la intersección de la ingeniería de software y la IA.',
  'about.bio2': 'Egresado de la Universidad de New Hampshire. Cuando no estoy escribiendo código, probablemente estoy con Bigsby — mi perro y copiloto leal.',
  'about.meetBigsby': 'Conoce a Bigsby',
  'about.captionBigsby': 'El mejor chico.',
  'about.captionTogether': 'Compañeros de aventuras.',
  'about.imgAltProfile': 'Jared Hooper',
  'about.imgAltBigsby': 'Bigsby el perro',
  'about.imgAltTogether': 'Jared y Bigsby',

  // Login page
  'login.title': 'Iniciar sesión',
  'login.heading': 'Bienvenido de nuevo',
  'login.subtitle': 'Inicia sesión en tu cuenta',
  'login.emailLabel': 'Correo electrónico',
  'login.emailPlaceholder': 'tu@ejemplo.com',
  'login.passwordLabel': 'Contraseña',
  'login.submit': 'Iniciar sesión',
  'login.noAccount': '¿No tienes una cuenta?',
  'login.signupLink': 'Regístrate',

  // Signup page
  'signup.title': 'Registrarse',
  'signup.heading': 'Crear cuenta',
  'signup.subtitle': 'Únete a Hooper Development',
  'signup.usernameLabel': 'Nombre de usuario',
  'signup.usernamePlaceholder': 'jsmith',
  'signup.nameLabel': 'Nombre completo',
  'signup.namePlaceholder': 'Juan Pérez',
  'signup.emailLabel': 'Correo electrónico',
  'signup.emailPlaceholder': 'tu@ejemplo.com',
  'signup.phoneLabel': 'Número de teléfono',
  'signup.phonePlaceholder': '(555) 123-4567',
  'signup.passwordLabel': 'Contraseña',
  'signup.submit': 'Crear cuenta',
  'signup.hasAccount': '¿Ya tienes una cuenta?',
  'signup.loginLink': 'Iniciar sesión',

  // Dashboard page
  'dashboard.title': 'Panel',
  'dashboard.heading': 'Panel',
  'dashboard.welcome': 'Bienvenido de nuevo, {name}',
  'dashboard.profile': 'Perfil',
  'dashboard.username': 'Usuario',
  'dashboard.email': 'Correo electrónico',
  'dashboard.phone': 'Teléfono',
  'dashboard.name': 'Nombre',
  'dashboard.memberSince': 'Miembro desde',
  'dashboard.lastLogin': 'Último acceso',
  'dashboard.logout': 'Cerrar sesión',
  'dashboard.sessions': 'Sesiones activas',
  'dashboard.sessionIp': 'Dirección IP',
  'dashboard.sessionAgent': 'Agente de usuario',
  'dashboard.sessionCreated': 'Creada',
  'dashboard.sessionExpires': 'Expira',
  'dashboard.noSessions': 'No hay sesiones activas.',

  // Lies wall
  'lies.title': 'Mentiras',
  'lies.heading': 'El Muro de Mentiras',
  'lies.subtitle': 'Cada secreto que fue expuesto. Cada visita muestra una página diferente.',
  'lies.empty': 'Aún no hay mentiras. Envía un secreto para comenzar.',
  'lies.meta': 'Página {page} de {pages} — {total} mentiras expuestas',
};

export default es;
