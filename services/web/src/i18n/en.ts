// English translations — source of truth for all UI strings.

const en = {
  // Brand
  'brand': 'Hooper Development',

  // Common
  'nav.home': 'Home',
  'nav.about': 'About',
  'nav.login': 'Login',
  'nav.signup': 'Sign Up',
  'nav.dashboard': 'Dashboard',
  'nav.logout': 'Logout',
  'nav.contact': 'Contact',
  'nav.lies': 'Lies',

  // MagicBar
  'magicbar.placeholder.hero': 'Where do you want to go?',
  'magicbar.placeholder.compact': 'Search...',
  'magicbar.section.navigate': 'Navigate',
  'magicbar.section.search': 'Search',
  'magicbar.searching': 'Searching...',
  'magicbar.noResults': 'No results',
  'magicbar.ariaLabel': 'Navigate or search',

  // MagicBar nav item descriptions
  'magicbar.desc.home': 'Go to homepage',
  'magicbar.desc.about': 'Learn about me',
  'magicbar.desc.login': 'Sign in to your account',
  'magicbar.desc.signup': 'Create a new account',
  'magicbar.desc.dashboard': 'View your dashboard',
  'magicbar.desc.logout': 'Sign out',
  'magicbar.desc.contact': 'Send me an email',
  'magicbar.desc.lies': 'Browse exposed secrets',

  // Homepage
  'home.brand': 'Hooper Development',

  // About page
  'about.title': 'About',
  'about.name': 'Jared Hooper',
  'about.jobTitle': 'Software Engineer at Outreach',
  'about.bio1': 'Software engineer based in Seattle, WA. I work at <a href="https://www.outreach.io" target="_blank" rel="noopener" style="color: var(--fresh-primary);">Outreach</a> and spend my free time building tools, agents, and infrastructure at the intersection of software engineering and AI.',
  'about.bio2': 'University of New Hampshire alum. When I\'m not writing code, I\'m probably hanging out with Bigsby — my dog and loyal co-pilot.',
  'about.meetBigsby': 'Meet Bigsby',
  'about.captionBigsby': 'The best boy.',
  'about.captionTogether': 'Partners in crime.',
  'about.imgAltProfile': 'Jared Hooper',
  'about.imgAltBigsby': 'Bigsby the dog',
  'about.imgAltTogether': 'Jared and Bigsby',

  // Login page
  'login.title': 'Login',
  'login.heading': 'Welcome Back',
  'login.subtitle': 'Sign in to your account',
  'login.emailLabel': 'Email',
  'login.emailPlaceholder': 'you@example.com',
  'login.passwordLabel': 'Password',
  'login.submit': 'Login',
  'login.noAccount': "Don't have an account?",
  'login.signupLink': 'Sign up',

  // Signup page
  'signup.title': 'Sign Up',
  'signup.heading': 'Create Account',
  'signup.subtitle': 'Join Hooper Development',
  'signup.usernameLabel': 'Username',
  'signup.usernamePlaceholder': 'jsmith',
  'signup.nameLabel': 'Full Name',
  'signup.namePlaceholder': 'John Smith',
  'signup.emailLabel': 'Email',
  'signup.emailPlaceholder': 'you@example.com',
  'signup.phoneLabel': 'Phone Number',
  'signup.phonePlaceholder': '(555) 123-4567',
  'signup.passwordLabel': 'Password',
  'signup.submit': 'Create Account',
  'signup.hasAccount': 'Already have an account?',
  'signup.loginLink': 'Login',

  // Dashboard page
  'dashboard.title': 'Dashboard',
  'dashboard.heading': 'Dashboard',
  'dashboard.welcome': 'Welcome back, {name}',
  'dashboard.profile': 'Profile',
  'dashboard.username': 'Username',
  'dashboard.email': 'Email',
  'dashboard.phone': 'Phone',
  'dashboard.name': 'Name',
  'dashboard.memberSince': 'Member since',
  'dashboard.lastLogin': 'Last login',
  'dashboard.logout': 'Logout',
  'dashboard.sessions': 'Active Sessions',
  'dashboard.sessionIp': 'IP Address',
  'dashboard.sessionAgent': 'User Agent',
  'dashboard.sessionCreated': 'Created',
  'dashboard.sessionExpires': 'Expires',
  'dashboard.noSessions': 'No active sessions.',

  // Lies wall
  'lies.title': 'Lies',
  'lies.heading': 'The Wall of Lies',
  'lies.subtitle': 'Every secret that was exposed. Each visit shows a different page.',
  'lies.empty': 'No lies yet. Submit a secret to begin.',
  'lies.meta': 'Page {page} of {pages} — {total} lies exposed',
} as const;

export type TranslationKey = keyof typeof en;
export default en;
