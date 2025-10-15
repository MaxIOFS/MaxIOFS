import UserDetailsPage from './index';

// Re-export the UserDetailsPage component to avoid redirect and page jump
// This ensures /users/:user/settings shows the same content as /users/:user
// without any navigation, eliminating the "jump" effect
export default UserDetailsPage;
