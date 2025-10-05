'use client';

import React, { useState } from 'react';
import { Bell, Search, Settings, User, LogOut, Menu } from 'lucide-react';
import { Button } from '@/components/ui/Button';
import { Input } from '@/components/ui/Input';
import { useAuth } from '@/hooks/useAuth';
import SweetAlert from '@/lib/sweetalert';

export interface HeaderProps {
  onMenuToggle?: () => void;
  showSearch?: boolean;
}

export function Header({ onMenuToggle, showSearch = true }: HeaderProps) {
  const { user, logout } = useAuth();
  const [searchQuery, setSearchQuery] = useState('');
  const [showUserMenu, setShowUserMenu] = useState(false);

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault();
    // TODO: Implement search functionality
  };

  const handleLogout = async () => {
    try {
      const result = await SweetAlert.confirmLogout();
      
      if (result.isConfirmed) {
        SweetAlert.loading('Signing out...', 'See you soon');
        await logout();
        SweetAlert.close();
      }
    } catch (error) {
      SweetAlert.close();
      SweetAlert.error('Error signing out', 'Could not sign out properly');
    }
  };

  return (
    <header className="bg-white shadow-sm border-b border-gray-200">
      <div className="flex items-center justify-between px-6 py-4">
        {/* Left section */}
        <div className="flex items-center space-x-4">
          {/* Mobile menu button */}
          <Button
            variant="ghost"
            size="icon"
            onClick={onMenuToggle}
            className="lg:hidden"
          >
            <Menu className="h-5 w-5" />
          </Button>

          {/* Logo visible en mobile cuando sidebar está cerrado */}
          <div className="flex items-center space-x-2 lg:hidden">
            {/* eslint-disable-next-line @next/next/no-img-element */}
            <img 
              src="/assets/img/icon.png" 
              alt="MaxIOFS" 
              className="w-7 h-7 rounded"
            />
            <span className="text-sm font-semibold text-gray-900">MaxIOFS</span>
          </div>

          {/* Search */}
          {showSearch && (
            <form onSubmit={handleSearch} className="hidden md:block">
              <Input
                type="text"
                placeholder="Search buckets, objects..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                leftIcon={<Search className="h-4 w-4" />}
                className="w-80"
              />
            </form>
          )}
        </div>

        {/* Right section */}
        <div className="flex items-center space-x-2">
          {/* Mobile search button */}
          {showSearch && (
            <Button
              variant="ghost"
              size="icon"
              className="md:hidden"
            >
              <Search className="h-5 w-5" />
            </Button>
          )}

          {/* Notifications */}
          <Button variant="ghost" size="icon" className="relative">
            <Bell className="h-5 w-5" />
            <span className="absolute -top-1 -right-1 h-4 w-4 bg-red-500 rounded-full text-xs text-white flex items-center justify-center">
              3
            </span>
          </Button>

          {/* Settings */}
          <Button variant="ghost" size="icon">
            <Settings className="h-5 w-5" />
          </Button>

          {/* User menu */}
          <div className="relative">
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setShowUserMenu(!showUserMenu)}
              className="relative"
            >
              <User className="h-5 w-5" />
            </Button>

            {/* User dropdown menu */}
            {showUserMenu && (
              <>
                {/* Backdrop */}
                <div
                  className="fixed inset-0 z-10"
                  onClick={() => setShowUserMenu(false)}
                />

                {/* Menu */}
                <div className="absolute right-0 mt-2 w-48 bg-white rounded-md shadow-lg border border-gray-200 z-20">
                  <div className="py-1">
                    {/* User info */}
                    <div className="px-4 py-2 border-b border-gray-100">
                      <p className="text-sm font-medium text-gray-900">
                        {user?.username || 'Unknown User'}
                      </p>
                      <p className="text-xs text-gray-500">
                        {user?.email || 'No email'}
                      </p>
                    </div>

                    {/* Menu items */}
                    <button
                      className="flex items-center w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
                      onClick={() => {
                        setShowUserMenu(false);
                        // TODO: Navigate to profile
                      }}
                    >
                      <User className="h-4 w-4 mr-2" />
                      Profile
                    </button>

                    <button
                      className="flex items-center w-full px-4 py-2 text-sm text-gray-700 hover:bg-gray-100"
                      onClick={() => {
                        setShowUserMenu(false);
                        // TODO: Navigate to settings
                      }}
                    >
                      <Settings className="h-4 w-4 mr-2" />
                      Settings
                    </button>

                    <div className="border-t border-gray-100">
                      <button
                        className="flex items-center w-full px-4 py-2 text-sm text-red-600 hover:bg-red-50"
                        onClick={() => {
                          setShowUserMenu(false);
                          handleLogout();
                        }}
                      >
                        <LogOut className="h-4 w-4 mr-2" />
                        Sign out
                      </button>
                    </div>
                  </div>
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </header>
  );
}

export default Header;